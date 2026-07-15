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

// フィルタ通過後の通知を UTC 日付 (YYYY-MM-DD) でビニングし、
// 日付昇順の時系列カウントを返す軽量集計エンドポイント。
//
// `/api/notifications/summary` が channel / status 別の合計を返すのに対し、
// 本エンドポイントは「いつどれだけ送られたか」の日次推移を返す。
// `/api/notifications` 全件取得 → クライアント集計に比べて、保持件数が
// 増えた状況でのペイロード量・JSON エンコード時間を削減する。
//
// バケットキーは `created_at` を UTC 化した `YYYY-MM-DD`。lex 昇順 = カレンダー
// 昇順を保つ（trilingual-gateway analytics-py の by_day / usermgmt-ts の by_day と同じ規約）。
// populated-only: 母集団 0 の日は含めない。破損した created_at (パース不能) は
// `applyListFilters` と同じ防御方針で集計対象外とする。
//
// `/:id` より前に登録して、`:id == "by_day"` の衝突を防ぐ。
app.get("/api/notifications/by_day", (req: Request, res: Response) => {
  const parsed = parseListFilters(req);
  if (!parsed.ok) {
    log("WARN", `Invalid by_day filter: ${parsed.error}`);
    res.status(parsed.status).json({ error: parsed.error });
    return;
  }

  const result = applyListFilters(notifications, parsed);

  // UTC 日付キー ("YYYY-MM-DD") → 件数。
  // 破損した created_at はスキップ（applyListFilters と同じ防御）。
  const counts = new Map<string, number>();
  let total = 0;
  for (const n of result) {
    const ts = new Date(n.created_at);
    if (Number.isNaN(ts.getTime())) continue;
    const key = ts.toISOString().slice(0, 10);
    counts.set(key, (counts.get(key) ?? 0) + 1);
    total += 1;
  }

  // 日付キーの lex 昇順 = カレンダー昇順（"2026-06-30" < "2026-07-01"）。
  // populated-only: 件数 0 の日は含めない。
  const byDay = Array.from(counts.entries())
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([day, count]) => ({ day, count }));

  log(
    "INFO",
    `by_day requested: total=${total} distinct_days=${byDay.length} user_id=${parsed.userId ?? "-"} channel=${parsed.channel ?? "-"} status=${parsed.status ?? "-"}`,
  );
  res.json({
    total,
    distinct_days: byDay.length,
    by_day: byDay,
  });
});

// フィルタ通過後の通知を UTC 時刻 ("00"〜"23") でビニングし、時刻昇順の
// 周期的カウントを返す軽量集計エンドポイント。
//
// `/api/notifications/by_day` が「いつ」流量があったかを直線時系列で見るのに対し、
// 本エンドポイントは「1 日のうち、どの時間帯に流量が集中しているか」を 1
// リクエストで把握する周期的集計。深夜バッチ / 朝の通知ピーク / 送信ワーカーの
// キャパシティプラン用途を想定する。既存 `/by_day` と同一のフィルタセット
// (`user_id` / `channel` / `status` / `since` / `until`) を再利用する。
//
// バケットキーは `created_at` を UTC 化した 2 桁ゼロ詰め時刻 (`"00"`〜`"23"`)。
// lex 順 = 時間順を保つ。populated-only: 母集団 0 の時間帯は含めない。
// 破損した created_at (パース不能) は `applyListFilters` と同じ防御方針で
// 集計対象外とする。
//
// `/:id` より前に登録して、`:id == "by_hour_of_day"` の衝突を防ぐ。
app.get("/api/notifications/by_hour_of_day", (req: Request, res: Response) => {
  const parsed = parseListFilters(req);
  if (!parsed.ok) {
    log("WARN", `Invalid by_hour_of_day filter: ${parsed.error}`);
    res.status(parsed.status).json({ error: parsed.error });
    return;
  }

  const result = applyListFilters(notifications, parsed);

  // UTC 時刻キー ("00"〜"23") → 件数。
  // 破損した created_at はスキップ（applyListFilters と同じ防御）。
  const counts = new Map<string, number>();
  let total = 0;
  for (const n of result) {
    const ts = new Date(n.created_at);
    if (Number.isNaN(ts.getTime())) continue;
    // toISOString() の 12〜13 文字目が UTC 時刻の 2 桁 (例: "2026-06-20T10:00:00.000Z" → "10")。
    // getUTCHours() を padStart しても同じだが、by_day が toISOString().slice(0,10) を使っており、
    // slice ベースで揃えることで tz 越境時の挙動を目視で追いやすくする。
    const key = ts.toISOString().slice(11, 13);
    counts.set(key, (counts.get(key) ?? 0) + 1);
    total += 1;
  }

  // 2 桁ゼロ詰め時刻 ("00"〜"23") は lex 順 = 時間順なので単純に sort で十分。
  const byHourOfDay = Array.from(counts.entries())
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([hour, count]) => ({ hour, count }));

  log(
    "INFO",
    `by_hour_of_day requested: total=${total} distinct_hours=${byHourOfDay.length} user_id=${parsed.userId ?? "-"} channel=${parsed.channel ?? "-"} status=${parsed.status ?? "-"}`,
  );
  res.json({
    total,
    distinct_hours: byHourOfDay.length,
    by_hour_of_day: byHourOfDay,
  });
});

// ISO 曜日番号 ("1"..."7") と UI 表示用 3 文字ラベルの対応表。
// 月曜始まりで日曜が最後。lex 昇順 ("1"..."7") = 月〜日順を保つ。
// 他リポジトリ (pulseboard-app api-gateway) の by_day_of_week と表記を揃える。
const WEEKDAY_NAMES: Record<string, string> = {
  "1": "Mon",
  "2": "Tue",
  "3": "Wed",
  "4": "Thu",
  "5": "Fri",
  "6": "Sat",
  "7": "Sun",
};

// フィルタ通過後の通知を UTC 曜日 ("1" 月曜〜"7" 日曜) でビニングし、
// 月〜日順の周期的カウントを返す軽量集計エンドポイント。
//
// `/api/notifications/by_hour_of_day` が「1 日のうちどの時間帯」の周期を見るのに対し、
// 本エンドポイントは「1 週間のうちどの曜日に流量が集中しているか」を 1 リクエストで
// 返す粗い周期軸。土日と平日で通知配信傾向が変わる SaaS 的ワークロードでの
// 週末フラット化検知や、月曜バッチ集中の可視化を想定する。既存 `/by_day` `/by_hour_of_day`
// と同一のフィルタセット (`user_id` / `channel` / `status` / `since` / `until`) を再利用する。
//
// バケットキーは `created_at` を UTC 化して算出した ISO 曜日番号 (`"1"` 月〜 `"7"` 日) の
// 文字列。`Date.getUTCDay()` は 0=Sun...6=Sat を返すので、`((getUTCDay() + 6) % 7) + 1`
// で月曜起点の 1..7 に写す。lex 昇順 = 月〜日順を保つ。各行には `day` (曜日番号) に加え、
// UI で存在チェック不要のラベル用に `weekday_name` ("Mon"..."Sun") を付ける。
// populated-only: 母集団 0 の曜日は含めない（`by_day` / `by_hour_of_day` と同じ規約）。
// 破損した created_at (パース不能) は `applyListFilters` と同じ防御方針で集計対象外とする。
//
// `/:id` より前に登録して、`:id == "by_day_of_week"` の衝突を防ぐ。
app.get("/api/notifications/by_day_of_week", (req: Request, res: Response) => {
  const parsed = parseListFilters(req);
  if (!parsed.ok) {
    log("WARN", `Invalid by_day_of_week filter: ${parsed.error}`);
    res.status(parsed.status).json({ error: parsed.error });
    return;
  }

  const result = applyListFilters(notifications, parsed);

  // ISO 曜日番号 ("1"..."7") → 件数。破損 created_at はスキップ。
  const counts = new Map<string, number>();
  let total = 0;
  for (const n of result) {
    const ts = new Date(n.created_at);
    if (Number.isNaN(ts.getTime())) continue;
    // getUTCDay(): 0 (Sun) 〜 6 (Sat) を、ISO 8601 の 1 (Mon) 〜 7 (Sun) に写像する。
    const iso = ((ts.getUTCDay() + 6) % 7) + 1;
    const key = String(iso);
    counts.set(key, (counts.get(key) ?? 0) + 1);
    total += 1;
  }

  // "1".."7" は lex 順 = 月〜日順なので単純 sort で十分。
  const byDayOfWeek = Array.from(counts.entries())
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([day, count]) => ({
      day,
      weekday_name: WEEKDAY_NAMES[day],
      count,
    }));

  log(
    "INFO",
    `by_day_of_week requested: total=${total} distinct_days_of_week=${byDayOfWeek.length} user_id=${parsed.userId ?? "-"} channel=${parsed.channel ?? "-"} status=${parsed.status ?? "-"}`,
  );
  res.json({
    total,
    distinct_days_of_week: byDayOfWeek.length,
    by_day_of_week: byDayOfWeek,
  });
});

// ISO 8601 週キー ("YYYY-Www") を UTC 基準で算出するヘルパ。
//
// JavaScript には Python の `strftime("%G-W%V")` に相当する組み込みがないため、
// 標準アルゴリズムで手計算する:
//   1. `d` を UTC の日付だけコピーして時刻を落とす（時刻依存で週跨ぎが揺れないよう）。
//   2. `d.getUTCDay()` は 0=Sun...6=Sat。ISO では 1=Mon...7=Sun なので `|| 7` で
//      0 (Sun) を 7 に写す。
//   3. ISO 週は「木曜日が属する週」の年・週番号を採用する規則。よって
//      `d` を最も近い木曜日 (dayNum-1 が差分、+4 で木曜) にシフトする。
//   4. その木曜日の属する年の 1/1 との差分日数 / 7 を切り上げると ISO 週番号。
//   5. 年跨ぎ規則: 12/29..12/31 が 1/1 と同一週にある場合、その週は翌年の W01。
//      逆に 1/1..1/3 が 12/31 と同一週にある場合、その週は前年の W53 (または W52)。
//      木曜シフト後の `d.getUTCFullYear()` がそのまま ISO 年になる。
function isoWeekKey(ts: Date): string {
  const d = new Date(Date.UTC(ts.getUTCFullYear(), ts.getUTCMonth(), ts.getUTCDate()));
  const dayNum = d.getUTCDay() || 7;
  d.setUTCDate(d.getUTCDate() + 4 - dayNum);
  const yearStart = new Date(Date.UTC(d.getUTCFullYear(), 0, 1));
  const weekNum = Math.ceil((((d.getTime() - yearStart.getTime()) / 86400000) + 1) / 7);
  return `${d.getUTCFullYear()}-W${String(weekNum).padStart(2, "0")}`;
}

// フィルタ通過後の通知を ISO 8601 週 ("YYYY-Www") でビニングし、週昇順の
// 線形時系列カウントを返す集計エンドポイント。
//
// `/api/notifications/by_day` が日次粒度、`/api/notifications/by_day_of_week` が
// 周期的曜日集計を返すのに対し、本エンドポイントは四半期・半期スパンでの
// 「週次流量トレンド」を 1 リクエストで返す中間解像度エンドポイント。日次だと
// 点が多過ぎ、曜日別だと期間全体の推移が見えないユースケース向け。
// 既存 `/by_day` `/by_hour_of_day` `/by_day_of_week` と同一のフィルタセット
// (`user_id` / `channel` / `status` / `since` / `until`) を再利用する。
//
// バケットキーは `created_at` を UTC 化した ISO 8601 週表記 (`"2026-W27"` 等)。
// ISO 週規則により週の初日は月曜、年跨ぎは木曜がどちらの年に属するかで決まる。
// 週番号は 2 桁ゼロ詰めで、"YYYY-Www" の lex 昇順 = カレンダー週昇順を保つ。
// populated-only: 母集団 0 の週は含めない（`by_day` と同じ規約）。
// 破損した created_at (パース不能) は `applyListFilters` と同じ防御方針で
// 集計対象外とする。
//
// `/:id` より前に登録して、`:id == "by_week"` の衝突を防ぐ。
app.get("/api/notifications/by_week", (req: Request, res: Response) => {
  const parsed = parseListFilters(req);
  if (!parsed.ok) {
    log("WARN", `Invalid by_week filter: ${parsed.error}`);
    res.status(parsed.status).json({ error: parsed.error });
    return;
  }

  const result = applyListFilters(notifications, parsed);

  // ISO 週キー ("YYYY-Www") → 件数。破損 created_at はスキップ。
  const counts = new Map<string, number>();
  let total = 0;
  for (const n of result) {
    const ts = new Date(n.created_at);
    if (Number.isNaN(ts.getTime())) continue;
    const key = isoWeekKey(ts);
    counts.set(key, (counts.get(key) ?? 0) + 1);
    total += 1;
  }

  // ISO 週フォーマット (YYYY-Www) は 2 桁ゼロ詰めで lex 順 = カレンダー週順。
  const byWeek = Array.from(counts.entries())
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([week, count]) => ({ week, count }));

  log(
    "INFO",
    `by_week requested: total=${total} distinct_weeks=${byWeek.length} user_id=${parsed.userId ?? "-"} channel=${parsed.channel ?? "-"} status=${parsed.status ?? "-"}`,
  );
  res.json({
    total,
    distinct_weeks: byWeek.length,
    by_week: byWeek,
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
