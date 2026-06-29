import express, { Request, Response, NextFunction } from "express";
import cors from "cors";
import { v4 as uuidv4 } from "uuid";

export interface Notification {
  id: string;
  user_id: string;
  channel: "email" | "sms" | "push";
  title: string;
  message: string;
  status: "pending" | "sent" | "failed";
  created_at: string;
}

const notifications: Notification[] = [];

// 1 プロセスで保持する通知の最大件数。0 以下なら無制限。
// 長時間稼働で in-memory store がメモリを無制限に占有するのを防ぐ。
function parseMaxNotifications(): number {
  const raw = process.env.MAX_NOTIFICATIONS;
  if (raw === undefined) return 10000;
  if (!/^-?\d+$/.test(raw)) {
    // 不正値はデフォルトにフォールバック（プロセスは継続）
    return 10000;
  }
  return parseInt(raw, 10);
}
// 起動時の cap。テスト時のみ `setMaxNotifications` で動的に上書きできる。
let maxNotifications = parseMaxNotifications();
export function setMaxNotifications(n: number): void {
  maxNotifications = n;
}
export function getMaxNotifications(): number {
  return maxNotifications;
}

// JSON ペイロードの最大サイズ。明示しないと express.json の既定 100kb で動くため、
// 環境変数で上書きできる形で明示する。
const MAX_REQUEST_BODY = process.env.MAX_REQUEST_BODY || "256kb";

export const app = express();
app.use(cors());
app.use(express.json({ limit: MAX_REQUEST_BODY }));

// ログレベル優先度。値が大きいほど重要度が高い。`currentLogLevel` 以上の
// 重要度を持つメッセージのみが出力される（INFO 設定なら DEBUG は抑止される）。
const LOG_LEVEL_PRIORITY: Record<string, number> = {
  DEBUG: 10,
  INFO: 20,
  WARN: 30,
  ERROR: 40,
};

// `LOG_LEVEL` 環境変数を解釈してレベル優先度に変換する。
// `user-api` の同名 env と運用を揃え、大文字小文字・前後空白の表記揺れを
// 吸収しつつ、不正値・空・未指定は INFO へフォールバックする。
export function parseLogLevel(raw: string | undefined | null): number {
  if (raw === undefined || raw === null) {
    return LOG_LEVEL_PRIORITY.INFO;
  }
  const normalized = raw.trim().toUpperCase();
  const priority = LOG_LEVEL_PRIORITY[normalized];
  if (typeof priority !== "number") {
    return LOG_LEVEL_PRIORITY.INFO;
  }
  return priority;
}

// 現在のログレベル。プロセス起動時に LOG_LEVEL から 1 回だけ読む。
// テストからは `setLogLevel(...)` で動的に上書きできる。
let currentLogLevel: number = parseLogLevel(process.env.LOG_LEVEL);

export function setLogLevel(level: string | undefined | null): void {
  currentLogLevel = parseLogLevel(level);
}

export function getLogLevel(): number {
  return currentLogLevel;
}

// 指定 level の重要度が `currentLogLevel` 以上のときのみ出力する。
// 例: currentLogLevel=INFO (20) のとき、DEBUG (10) は出力されない。
// 未知のレベル文字列は安全側に倒して INFO 相当として扱う（既存呼び出しの後方互換）。
const log = (level: string, msg: string) => {
  const priority = LOG_LEVEL_PRIORITY[level.toUpperCase()] ?? LOG_LEVEL_PRIORITY.INFO;
  if (priority < currentLogLevel) {
    return;
  }
  const ts = new Date().toISOString();
  console.log(`${ts} [${level}] notification-service: ${msg}`);
};

app.get("/health", (_req: Request, res: Response) => {
  // /health は K8s probe / ロードバランサから高頻度に呼ばれるため、
  // DEBUG レベルでのみ出力し既定運用ではノイズに埋もれないようにする。
  // `user-api` の `logger.debug("Health check requested")` と運用を揃える。
  log("DEBUG", "Health check requested");
  res.json({
    status: "healthy",
    service: "notification-service",
    timestamp: new Date().toISOString(),
  });
});

app.post("/api/notifications/send", (req: Request, res: Response) => {
  const { user_id, channel, title, message } = req.body;

  if (!user_id || !channel || !title || !message) {
    log("WARN", "Send attempt with missing fields");
    res.status(400).json({ error: "user_id, channel, title, and message are required" });
    return;
  }

  const validChannels = ["email", "sms", "push"];
  if (!validChannels.includes(channel)) {
    log("WARN", `Invalid channel: ${channel}`);
    res.status(400).json({ error: `Invalid channel. Must be one of: ${validChannels.join(", ")}` });
    return;
  }

  const notification: Notification = {
    id: uuidv4(),
    user_id,
    channel,
    title,
    message,
    status: "sent",
    created_at: new Date().toISOString(),
  };

  notifications.push(notification);
  // FIFO eviction: 上限を超えたら先頭から破棄する。
  // 0 以下は無制限扱い（テスト用に環境変数で完全無効化できる）。
  if (maxNotifications > 0 && notifications.length > maxNotifications) {
    const overflow = notifications.length - maxNotifications;
    notifications.splice(0, overflow);
    log(
      "INFO",
      `Evicted ${overflow} old notification(s) (cap=${maxNotifications})`,
    );
  }
  log("INFO", `Notification sent: id=${notification.id} channel=${channel} user=${user_id}`);
  res.status(201).json(notification);
});

// ページネーションの既定値・上限。
const DEFAULT_LIMIT = 50;
const MAX_LIMIT = 100;

// GET /api/notifications でフィルタとして許容する値。
// POST /api/notifications/send の validChannels と整合させる。
const ALLOWED_CHANNELS: Notification["channel"][] = ["email", "sms", "push"];
const ALLOWED_STATUSES: Notification["status"][] = ["pending", "sent", "failed"];

// 単一スカラのクエリ値のみ受理する（配列・オブジェクトは無効扱い）。
// 未指定は undefined を返し、無効型なら null を返す（呼び出し側が 400）。
function parseSingleScalarParam(raw: unknown): string | null | undefined {
  if (raw === undefined) {
    return undefined;
  }
  if (typeof raw !== "string") {
    return null;
  }
  return raw;
}

// parseIsoDateTime は ISO 8601 / RFC 3339 文字列を Date に変換する。
// `Z` サフィックスは `+00:00` として扱う。空白のみ・未パースは Error。
// `created_at` は POST 時に `new Date().toISOString()` で UTC タイムゾーン付き
// 文字列を書き込んでいるため、フィルタ側も UTC として比較する。
function parseIsoDateTime(value: string, field: string): Date {
  const trimmed = value.trim();
  if (trimmed.length === 0) {
    throw new Error(`${field} must not be blank`);
  }
  const normalized = trimmed.endsWith("Z")
    ? `${trimmed.slice(0, -1)}+00:00`
    : trimmed;
  const d = new Date(normalized);
  if (Number.isNaN(d.getTime())) {
    throw new Error(`${field} must be an ISO 8601 / RFC 3339 datetime`);
  }
  return d;
}

// parsePaginationParam はクエリ文字列を整数として検証する。
// 未指定なら fallback を返し、不正値（数値でない・範囲外）なら null を返す。
function parsePaginationParam(
  raw: string | undefined,
  fallback: number,
  min: number,
  max: number,
): number | null {
  if (raw === undefined) {
    return fallback;
  }
  // 整数のみ許可（小数や指数表記、空文字を拒否）。
  if (!/^-?\d+$/.test(raw)) {
    return null;
  }
  const value = parseInt(raw, 10);
  if (value < min || value > max) {
    return null;
  }
  return value;
}

// parseListFilters は GET /api/notifications と /summary が共有する
// フィルタクエリ（user_id / channel / status / since / until）を解析する。
// 失敗時は HTTP ステータスとエラーメッセージのペアを返す。
function parseListFilters(req: Request):
  | {
      ok: true;
      userId?: string;
      channel?: Notification["channel"];
      status?: Notification["status"];
      since?: Date;
      until?: Date;
    }
  | { ok: false; status: number; error: string } {
  const userId = req.query.user_id as string | undefined;

  // channel / status は単一スカラのみ受理し、空文字や配列・オブジェクトは
  // 「フィルタ無効」とする（部分的な絞り込み事故を避けるため）。
  const channelRaw = parseSingleScalarParam(req.query.channel);
  if (channelRaw === null || channelRaw === "") {
    return {
      ok: false,
      status: 400,
      error: `channel must be one of: ${ALLOWED_CHANNELS.join(", ")}`,
    };
  }
  if (channelRaw !== undefined && !ALLOWED_CHANNELS.includes(channelRaw as Notification["channel"])) {
    return {
      ok: false,
      status: 400,
      error: `channel must be one of: ${ALLOWED_CHANNELS.join(", ")}`,
    };
  }

  const statusRaw = parseSingleScalarParam(req.query.status);
  if (statusRaw === null || statusRaw === "") {
    return {
      ok: false,
      status: 400,
      error: `status must be one of: ${ALLOWED_STATUSES.join(", ")}`,
    };
  }
  if (statusRaw !== undefined && !ALLOWED_STATUSES.includes(statusRaw as Notification["status"])) {
    return {
      ok: false,
      status: 400,
      error: `status must be one of: ${ALLOWED_STATUSES.join(", ")}`,
    };
  }

  let since: Date | undefined;
  let until: Date | undefined;
  const sinceRaw = req.query.since;
  if (sinceRaw !== undefined) {
    if (typeof sinceRaw !== "string") {
      return { ok: false, status: 400, error: "since must be a single string" };
    }
    try {
      since = parseIsoDateTime(sinceRaw, "since");
    } catch (e) {
      return { ok: false, status: 400, error: (e as Error).message };
    }
  }
  const untilRaw = req.query.until;
  if (untilRaw !== undefined) {
    if (typeof untilRaw !== "string") {
      return { ok: false, status: 400, error: "until must be a single string" };
    }
    try {
      until = parseIsoDateTime(untilRaw, "until");
    } catch (e) {
      return { ok: false, status: 400, error: (e as Error).message };
    }
  }
  if (since && until && until < since) {
    return {
      ok: false,
      status: 400,
      error: "until must be greater than or equal to since",
    };
  }

  return {
    ok: true,
    userId,
    channel: channelRaw as Notification["channel"] | undefined,
    status: statusRaw as Notification["status"] | undefined,
    since,
    until,
  };
}

function applyListFilters(
  source: Notification[],
  f: {
    userId?: string;
    channel?: Notification["channel"];
    status?: Notification["status"];
    since?: Date;
    until?: Date;
  },
): Notification[] {
  let result = source;
  if (f.userId) {
    result = result.filter((n) => n.user_id === f.userId);
  }
  if (f.channel !== undefined) {
    result = result.filter((n) => n.channel === f.channel);
  }
  if (f.status !== undefined) {
    result = result.filter((n) => n.status === f.status);
  }
  if (f.since !== undefined || f.until !== undefined) {
    result = result.filter((n) => {
      // created_at は POST 時に new Date().toISOString() で書き込んでいるため
      // パース失敗は通常起き得ないが、保険として除外扱いにする
      const ts = new Date(n.created_at);
      if (Number.isNaN(ts.getTime())) {
        return false;
      }
      if (f.since !== undefined && ts < f.since) return false;
      if (f.until !== undefined && ts > f.until) return false;
      return true;
    });
  }
  return result;
}

app.get("/api/notifications", (req: Request, res: Response) => {
  const parsed = parseListFilters(req);
  if (!parsed.ok) {
    log("WARN", `Invalid filter: ${parsed.error}`);
    res.status(parsed.status).json({ error: parsed.error });
    return;
  }

  const limit = parsePaginationParam(req.query.limit as string | undefined, DEFAULT_LIMIT, 1, MAX_LIMIT);
  if (limit === null) {
    log("WARN", `Invalid limit: ${req.query.limit}`);
    res.status(400).json({ error: `limit must be an integer between 1 and ${MAX_LIMIT}` });
    return;
  }

  const offset = parsePaginationParam(req.query.offset as string | undefined, 0, 0, Number.MAX_SAFE_INTEGER);
  if (offset === null) {
    log("WARN", `Invalid offset: ${req.query.offset}`);
    res.status(400).json({ error: "offset must be a non-negative integer" });
    return;
  }

  const result = applyListFilters(notifications, parsed);
  const page = result.slice(offset, offset + limit);
  log(
    "INFO",
    `Listing notifications: ${page.length} returned (total=${result.length} limit=${limit} offset=${offset} channel=${parsed.channel ?? "-"} status=${parsed.status ?? "-"})`,
  );
  res.json(page);
});

// チャネル / ステータス別の通知件数を 1 リクエストで取得する集計エンドポイント。
// `by_channel` / `by_status` は ALLOWED_* の全キーを 0 で初期化して返すため、
// クライアントは存在チェックなしで各カテゴリにアクセスできる。
app.get("/api/notifications/summary", (req: Request, res: Response) => {
  const parsed = parseListFilters(req);
  if (!parsed.ok) {
    log("WARN", `Invalid summary filter: ${parsed.error}`);
    res.status(parsed.status).json({ error: parsed.error });
    return;
  }

  const result = applyListFilters(notifications, parsed);
  const byChannel: Record<Notification["channel"], number> = {
    email: 0,
    sms: 0,
    push: 0,
  };
  const byStatus: Record<Notification["status"], number> = {
    pending: 0,
    sent: 0,
    failed: 0,
  };
  for (const n of result) {
    byChannel[n.channel] += 1;
    byStatus[n.status] += 1;
  }

  log("INFO", `Summary requested: total=${result.length}`);
  res.json({
    total: result.length,
    by_channel: byChannel,
    by_status: byStatus,
  });
});

// 指定フィルタに一致する通知を一括削除する。
// 誤った全件削除を防ぐため、`user_id` / `channel` / `status` / `since` / `until`
// の少なくとも 1 つを必須にする（analytics-engine の deleteEventsHandler と同じ規約）。
// GET と同じ `parseListFilters` を再利用するため、バリデーション挙動・エラーメッセージは
// 完全に一致する。
app.delete("/api/notifications", (req: Request, res: Response) => {
  const parsed = parseListFilters(req);
  if (!parsed.ok) {
    log("WARN", `Invalid delete filter: ${parsed.error}`);
    res.status(parsed.status).json({ error: parsed.error });
    return;
  }

  const hasAnyFilter =
    parsed.userId !== undefined ||
    parsed.channel !== undefined ||
    parsed.status !== undefined ||
    parsed.since !== undefined ||
    parsed.until !== undefined;
  if (!hasAnyFilter) {
    log("WARN", "Delete attempt without any filter");
    res.status(400).json({
      error:
        "at least one of 'user_id', 'channel', 'status', 'since', or 'until' must be provided",
    });
    return;
  }

  const before = notifications.length;
  // 末尾から走査して splice することで、削除中の index 衝突を避ける。
  for (let i = notifications.length - 1; i >= 0; i--) {
    const n = notifications[i];
    if (parsed.userId !== undefined && n.user_id !== parsed.userId) continue;
    if (parsed.channel !== undefined && n.channel !== parsed.channel) continue;
    if (parsed.status !== undefined && n.status !== parsed.status) continue;
    if (parsed.since !== undefined || parsed.until !== undefined) {
      const ts = new Date(n.created_at);
      // 破損した created_at は誤削除回避のため対象外とする
      if (Number.isNaN(ts.getTime())) continue;
      if (parsed.since !== undefined && ts < parsed.since) continue;
      if (parsed.until !== undefined && ts > parsed.until) continue;
    }
    notifications.splice(i, 1);
  }
  const deleted = before - notifications.length;
  log(
    "INFO",
    `Notifications deleted: count=${deleted} user_id=${parsed.userId ?? "-"} channel=${parsed.channel ?? "-"} status=${parsed.status ?? "-"}`,
  );
  res.json({
    deleted,
    user_id: parsed.userId ?? null,
    channel: parsed.channel ?? null,
    status: parsed.status ?? null,
    since: parsed.since ? parsed.since.toISOString() : null,
    until: parsed.until ? parsed.until.toISOString() : null,
  });
});

app.get("/api/notifications/:id", (req: Request, res: Response) => {
  const notification = notifications.find((n) => n.id === req.params.id);
  if (!notification) {
    log("WARN", `Notification not found: ${req.params.id}`);
    res.status(404).json({ error: "Notification not found" });
    return;
  }
  log("INFO", `Notification retrieved: ${req.params.id}`);
  res.json(notification);
});

// express.json の limit 超過は SyntaxError ではなく entity.too.large になる。
// 既定の Express エラーハンドラに任せると HTML を返してしまうため、
// JSON で 413 を返す専用ハンドラをアプリ末尾に登録する。
// 同様に、構文不正な JSON ボディは body-parser が SyntaxError
// (`type === 'entity.parse.failed'`) として投げる。これも既定ハンドラに
// 任せると HTML 500 を返してしまい、クライアント起因の不正リクエストで
// SRE 5xx アラートが誤発火するため、JSON で 400 を返すハンドラを並べる。
app.use(
  (
    err: Error & { type?: string; status?: number; statusCode?: number },
    req: Request,
    res: Response,
    next: NextFunction,
  ) => {
    const status = err.status ?? err.statusCode;
    if (err && (err.type === "entity.too.large" || status === 413)) {
      log("WARN", `Request body too large (limit=${MAX_REQUEST_BODY})`);
      res.status(413).json({ error: "request body too large" });
      return;
    }
    if (err instanceof SyntaxError && err.type === "entity.parse.failed") {
      log("WARN", `Malformed JSON body on ${req.method} ${req.path}`);
      res.status(400).json({ error: "invalid JSON body" });
      return;
    }
    next(err);
  },
);

export function clearNotifications() {
  notifications.length = 0;
}

export { MAX_REQUEST_BODY };

if (require.main === module) {
  const port = parseInt(process.env.NOTIFICATION_PORT || "5003", 10);
  app.listen(port, "0.0.0.0", () => {
    log("INFO", `Starting notification-service on port ${port}`);
  });
}
