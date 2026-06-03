import express, { Request, Response } from "express";
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

export const app = express();
app.use(cors());
app.use(express.json());

const log = (level: string, msg: string) => {
  const ts = new Date().toISOString();
  console.log(`${ts} [${level}] notification-service: ${msg}`);
};

app.get("/health", (_req: Request, res: Response) => {
  log("INFO", "Health check requested");
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

export function clearNotifications() {
  notifications.length = 0;
}

if (require.main === module) {
  const port = parseInt(process.env.NOTIFICATION_PORT || "5003", 10);
  app.listen(port, "0.0.0.0", () => {
    log("INFO", `Starting notification-service on port ${port}`);
  });
}
