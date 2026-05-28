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

app.get("/api/notifications", (req: Request, res: Response) => {
  const userId = req.query.user_id as string | undefined;

  // channel / status は単一スカラのみ受理し、空文字や配列・オブジェクトは
  // 「フィルタ無効」とする（部分的な絞り込み事故を避けるため）。
  const channelRaw = parseSingleScalarParam(req.query.channel);
  if (channelRaw === null || channelRaw === "") {
    log("WARN", `Invalid channel filter: ${JSON.stringify(req.query.channel)}`);
    res.status(400).json({
      error: `channel must be one of: ${ALLOWED_CHANNELS.join(", ")}`,
    });
    return;
  }
  if (channelRaw !== undefined && !ALLOWED_CHANNELS.includes(channelRaw as Notification["channel"])) {
    log("WARN", `Invalid channel filter: ${channelRaw}`);
    res.status(400).json({
      error: `channel must be one of: ${ALLOWED_CHANNELS.join(", ")}`,
    });
    return;
  }

  const statusRaw = parseSingleScalarParam(req.query.status);
  if (statusRaw === null || statusRaw === "") {
    log("WARN", `Invalid status filter: ${JSON.stringify(req.query.status)}`);
    res.status(400).json({
      error: `status must be one of: ${ALLOWED_STATUSES.join(", ")}`,
    });
    return;
  }
  if (statusRaw !== undefined && !ALLOWED_STATUSES.includes(statusRaw as Notification["status"])) {
    log("WARN", `Invalid status filter: ${statusRaw}`);
    res.status(400).json({
      error: `status must be one of: ${ALLOWED_STATUSES.join(", ")}`,
    });
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

  let result: Notification[] = notifications;
  if (userId) {
    result = result.filter((n) => n.user_id === userId);
  }
  if (channelRaw !== undefined) {
    result = result.filter((n) => n.channel === channelRaw);
  }
  if (statusRaw !== undefined) {
    result = result.filter((n) => n.status === statusRaw);
  }

  const page = result.slice(offset, offset + limit);
  log(
    "INFO",
    `Listing notifications: ${page.length} returned (total=${result.length} limit=${limit} offset=${offset} channel=${channelRaw ?? "-"} status=${statusRaw ?? "-"})`,
  );
  res.json(page);
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
