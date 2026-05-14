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

app.get("/api/notifications", (req: Request, res: Response) => {
  const userId = req.query.user_id as string | undefined;
  let result = notifications;
  if (userId) {
    result = notifications.filter((n) => n.user_id === userId);
  }
  log("INFO", `Listing notifications: ${result.length} found`);
  res.json(result);
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
