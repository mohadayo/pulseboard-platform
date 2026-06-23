import request from "supertest";
import {
  app,
  clearNotifications,
  getMaxNotifications,
  setMaxNotifications,
} from "./index";

beforeEach(() => {
  clearNotifications();
});

describe("GET /health", () => {
  it("returns healthy status", async () => {
    const res = await request(app).get("/health");
    expect(res.status).toBe(200);
    expect(res.body.status).toBe("healthy");
    expect(res.body.service).toBe("notification-service");
  });
});

describe("POST /api/notifications/send", () => {
  it("sends a notification successfully", async () => {
    const res = await request(app).post("/api/notifications/send").send({
      user_id: "u1",
      channel: "email",
      title: "Welcome",
      message: "Hello there!",
    });
    expect(res.status).toBe(201);
    expect(res.body.user_id).toBe("u1");
    expect(res.body.channel).toBe("email");
    expect(res.body.status).toBe("sent");
    expect(res.body.id).toBeDefined();
  });

  it("rejects missing fields", async () => {
    const res = await request(app).post("/api/notifications/send").send({
      user_id: "u1",
    });
    expect(res.status).toBe(400);
  });

  it("rejects invalid channel", async () => {
    const res = await request(app).post("/api/notifications/send").send({
      user_id: "u1",
      channel: "telegram",
      title: "Test",
      message: "Test msg",
    });
    expect(res.status).toBe(400);
  });
});

describe("GET /api/notifications", () => {
  it("lists all notifications", async () => {
    await request(app).post("/api/notifications/send").send({
      user_id: "u1",
      channel: "email",
      title: "T1",
      message: "M1",
    });
    await request(app).post("/api/notifications/send").send({
      user_id: "u2",
      channel: "sms",
      title: "T2",
      message: "M2",
    });

    const res = await request(app).get("/api/notifications");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(2);
  });

  it("filters by user_id", async () => {
    await request(app).post("/api/notifications/send").send({
      user_id: "u1",
      channel: "email",
      title: "T1",
      message: "M1",
    });
    await request(app).post("/api/notifications/send").send({
      user_id: "u2",
      channel: "sms",
      title: "T2",
      message: "M2",
    });

    const res = await request(app).get("/api/notifications?user_id=u1");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(1);
    expect(res.body[0].user_id).toBe("u1");
  });
});

describe("GET /api/notifications pagination", () => {
  // テスト用に複数件の通知を作成するヘルパー。
  async function seed(count: number): Promise<void> {
    for (let i = 0; i < count; i++) {
      await request(app).post("/api/notifications/send").send({
        user_id: "u1",
        channel: "email",
        title: `T${i}`,
        message: `M${i}`,
      });
    }
  }

  it("applies the limit parameter", async () => {
    await seed(5);
    const res = await request(app).get("/api/notifications?limit=2");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(2);
    expect(res.body[0].title).toBe("T0");
    expect(res.body[1].title).toBe("T1");
  });

  it("applies the offset parameter", async () => {
    await seed(5);
    const res = await request(app).get("/api/notifications?limit=2&offset=2");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(2);
    expect(res.body[0].title).toBe("T2");
    expect(res.body[1].title).toBe("T3");
  });

  it("returns empty array when offset is beyond available count", async () => {
    await seed(3);
    const res = await request(app).get("/api/notifications?offset=10");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(0);
  });

  it("defaults to at most 50 results when limit is not given", async () => {
    await seed(60);
    const res = await request(app).get("/api/notifications");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(50);
  });

  it("combines pagination with user_id filtering", async () => {
    await seed(3);
    await request(app).post("/api/notifications/send").send({
      user_id: "u2",
      channel: "sms",
      title: "other",
      message: "m",
    });
    const res = await request(app).get("/api/notifications?user_id=u1&limit=2&offset=1");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(2);
    expect(res.body.every((n: { user_id: string }) => n.user_id === "u1")).toBe(true);
    expect(res.body[0].title).toBe("T1");
  });

  it("rejects a non-numeric limit", async () => {
    const res = await request(app).get("/api/notifications?limit=abc");
    expect(res.status).toBe(400);
  });

  it("rejects a limit above the maximum", async () => {
    const res = await request(app).get("/api/notifications?limit=101");
    expect(res.status).toBe(400);
  });

  it("rejects a limit of zero", async () => {
    const res = await request(app).get("/api/notifications?limit=0");
    expect(res.status).toBe(400);
  });

  it("rejects a negative offset", async () => {
    const res = await request(app).get("/api/notifications?offset=-1");
    expect(res.status).toBe(400);
  });
});

describe("GET /api/notifications channel/status filters", () => {
  async function seedMixed(): Promise<void> {
    await request(app).post("/api/notifications/send").send({
      user_id: "u1",
      channel: "email",
      title: "T-email-1",
      message: "M",
    });
    await request(app).post("/api/notifications/send").send({
      user_id: "u1",
      channel: "sms",
      title: "T-sms-1",
      message: "M",
    });
    await request(app).post("/api/notifications/send").send({
      user_id: "u2",
      channel: "push",
      title: "T-push-1",
      message: "M",
    });
    await request(app).post("/api/notifications/send").send({
      user_id: "u2",
      channel: "email",
      title: "T-email-2",
      message: "M",
    });
  }

  it("filters by channel=email", async () => {
    await seedMixed();
    const res = await request(app).get("/api/notifications?channel=email");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(2);
    expect(res.body.every((n: { channel: string }) => n.channel === "email")).toBe(true);
  });

  it("filters by channel=push", async () => {
    await seedMixed();
    const res = await request(app).get("/api/notifications?channel=push");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(1);
    expect(res.body[0].channel).toBe("push");
  });

  it("rejects unknown channel value", async () => {
    const res = await request(app).get("/api/notifications?channel=telegram");
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("channel");
  });

  it("rejects empty channel value", async () => {
    const res = await request(app).get("/api/notifications?channel=");
    expect(res.status).toBe(400);
  });

  it("filters by status=sent", async () => {
    await seedMixed();
    const res = await request(app).get("/api/notifications?status=sent");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(4);
    expect(res.body.every((n: { status: string }) => n.status === "sent")).toBe(true);
  });

  it("rejects unknown status value", async () => {
    const res = await request(app).get("/api/notifications?status=draft");
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("status");
  });

  it("combines channel + user_id filters (AND)", async () => {
    await seedMixed();
    const res = await request(app).get("/api/notifications?channel=email&user_id=u2");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(1);
    expect(res.body[0].channel).toBe("email");
    expect(res.body[0].user_id).toBe("u2");
  });

  it("combines channel + status + pagination", async () => {
    await seedMixed();
    const res = await request(app).get("/api/notifications?channel=email&status=sent&limit=1&offset=1");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(1);
    expect(res.body[0].channel).toBe("email");
  });

  it("returns empty array when no notifications match the filter", async () => {
    await seedMixed();
    const res = await request(app).get("/api/notifications?channel=sms&user_id=u2");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(0);
  });

  it("rejects channel as array (duplicated query)", async () => {
    const res = await request(app).get("/api/notifications?channel=email&channel=sms");
    expect(res.status).toBe(400);
  });
});

describe("GET /api/notifications/:id", () => {
  it("retrieves a notification by id", async () => {
    const sendRes = await request(app).post("/api/notifications/send").send({
      user_id: "u1",
      channel: "push",
      title: "Alert",
      message: "Something happened",
    });
    const id = sendRes.body.id;

    const res = await request(app).get(`/api/notifications/${id}`);
    expect(res.status).toBe(200);
    expect(res.body.id).toBe(id);
  });

  it("returns 404 for unknown id", async () => {
    const res = await request(app).get("/api/notifications/unknown-id");
    expect(res.status).toBe(404);
  });
});

describe("GET /api/notifications since/until time-range filter", () => {
  beforeEach(() => {
    clearNotifications();
  });

  // テストは送信時刻（UTC ISO）を直接使うのではなく、POST 直前/直後の
  // 現在時刻を `since`/`until` に渡して挙動を検証する。
  const send = (overrides: Record<string, unknown> = {}) =>
    request(app)
      .post("/api/notifications/send")
      .send({
        user_id: "u1",
        channel: "email",
        title: "T",
        message: "M",
        ...overrides,
      });

  it("filters by since (inclusive lower bound)", async () => {
    await send({ title: "before" });
    // since にこの直後の時刻を使うため、ms 単位で安定するよう少し進めた値を用意
    const cutoff = new Date(Date.now() + 1).toISOString();
    // 後続を送ってから since=cutoff で絞る → 後続のみ
    await new Promise((r) => setTimeout(r, 10));
    await send({ title: "after-1" });
    await send({ title: "after-2" });
    const res = await request(app).get(
      `/api/notifications?since=${encodeURIComponent(cutoff)}`
    );
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(2);
    expect(res.body.every((n: { title: string }) => n.title.startsWith("after"))).toBe(true);
  });

  it("filters by until (inclusive upper bound)", async () => {
    await send({ title: "before-1" });
    await send({ title: "before-2" });
    await new Promise((r) => setTimeout(r, 10));
    const cutoff = new Date(Date.now()).toISOString();
    await new Promise((r) => setTimeout(r, 10));
    await send({ title: "after-1" });
    const res = await request(app).get(
      `/api/notifications?until=${encodeURIComponent(cutoff)}`
    );
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(2);
    expect(res.body.every((n: { title: string }) => n.title.startsWith("before"))).toBe(true);
  });

  it("filters by since and until combined", async () => {
    await send({ title: "early" });
    await new Promise((r) => setTimeout(r, 10));
    const lo = new Date(Date.now()).toISOString();
    await new Promise((r) => setTimeout(r, 10));
    await send({ title: "middle" });
    await new Promise((r) => setTimeout(r, 10));
    const hi = new Date(Date.now()).toISOString();
    await new Promise((r) => setTimeout(r, 10));
    await send({ title: "late" });
    const res = await request(app).get(
      `/api/notifications?since=${encodeURIComponent(lo)}&until=${encodeURIComponent(hi)}`
    );
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(1);
    expect(res.body[0].title).toBe("middle");
  });

  it("rejects invalid since with 400", async () => {
    const res = await request(app).get("/api/notifications?since=not-a-date");
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("since");
  });

  it("rejects invalid until with 400", async () => {
    const res = await request(app).get("/api/notifications?until=foo");
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("until");
  });

  it("rejects blank since with 400", async () => {
    const res = await request(app).get("/api/notifications?since=%20%20%20");
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("blank");
  });

  it("rejects since > until with 400", async () => {
    const res = await request(app).get(
      "/api/notifications?since=2026-01-01T00:00:00Z&until=2024-01-01T00:00:00Z"
    );
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("until");
  });

  it("accepts Z-suffixed UTC timestamps", async () => {
    // 過去日時を since にすれば全件マッチ
    await send({ title: "x" });
    const res = await request(app).get(
      "/api/notifications?since=2000-01-01T00:00:00Z"
    );
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(1);
  });
});

describe("GET /api/notifications/summary", () => {
  beforeEach(() => {
    clearNotifications();
  });

  const send = (channel: "email" | "sms" | "push", user_id = "u1") =>
    request(app).post("/api/notifications/send").send({
      user_id,
      channel,
      title: "T",
      message: "M",
    });

  it("returns zero counts when the store is empty", async () => {
    const res = await request(app).get("/api/notifications/summary");
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(0);
    expect(res.body.by_channel).toEqual({ email: 0, sms: 0, push: 0 });
    expect(res.body.by_status).toEqual({ pending: 0, sent: 0, failed: 0 });
  });

  it("aggregates by channel and status", async () => {
    await send("email");
    await send("email");
    await send("sms");
    await send("push");
    const res = await request(app).get("/api/notifications/summary");
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(4);
    expect(res.body.by_channel).toEqual({ email: 2, sms: 1, push: 1 });
    // POST 時点では status="sent" のみ
    expect(res.body.by_status).toEqual({ pending: 0, sent: 4, failed: 0 });
  });

  it("respects user_id filter", async () => {
    await send("email", "u1");
    await send("email", "u1");
    await send("sms", "u2");
    const res = await request(app).get("/api/notifications/summary?user_id=u1");
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(2);
    expect(res.body.by_channel.email).toBe(2);
    expect(res.body.by_channel.sms).toBe(0);
  });

  it("respects channel filter", async () => {
    await send("email");
    await send("sms");
    await send("push");
    const res = await request(app).get("/api/notifications/summary?channel=email");
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(1);
    expect(res.body.by_channel).toEqual({ email: 1, sms: 0, push: 0 });
  });

  it("rejects invalid channel filter with 400", async () => {
    const res = await request(app).get("/api/notifications/summary?channel=bogus");
    expect(res.status).toBe(400);
  });

  it("respects since/until filters", async () => {
    await send("email");
    await new Promise((r) => setTimeout(r, 10));
    const cutoff = new Date(Date.now()).toISOString();
    await new Promise((r) => setTimeout(r, 10));
    await send("sms");
    const res = await request(app).get(
      `/api/notifications/summary?since=${encodeURIComponent(cutoff)}`
    );
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(1);
    expect(res.body.by_channel.sms).toBe(1);
    expect(res.body.by_channel.email).toBe(0);
  });
});

describe("In-memory store cap (MAX_NOTIFICATIONS)", () => {
  const ORIGINAL_CAP = getMaxNotifications();
  afterEach(() => {
    setMaxNotifications(ORIGINAL_CAP);
  });

  async function send(userId: string): Promise<void> {
    await request(app).post("/api/notifications/send").send({
      user_id: userId,
      channel: "email",
      title: "t",
      message: "m",
    });
  }

  it("evicts oldest entries when cap is exceeded (FIFO)", async () => {
    setMaxNotifications(3);
    await send("u1");
    await send("u2");
    await send("u3");
    await send("u4");

    const res = await request(app).get("/api/notifications");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(3);
    // FIFO: u1 が落ち、残るのは u2/u3/u4
    expect(res.body.map((n: { user_id: string }) => n.user_id)).toEqual([
      "u2",
      "u3",
      "u4",
    ]);
  });

  it("keeps everything when cap is 0 (unlimited)", async () => {
    setMaxNotifications(0);
    for (let i = 0; i < 5; i += 1) {
      await send(`u${i}`);
    }
    const res = await request(app).get("/api/notifications");
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(5);
  });
});

describe("JSON body size limit", () => {
  it("returns 413 with JSON body when POST body exceeds the configured limit", async () => {
    // 既定 256kb を確実に超える 512KB の payload を組み立てる。
    const huge = "a".repeat(512 * 1024);
    const res = await request(app)
      .post("/api/notifications/send")
      .set("Content-Type", "application/json")
      .send({ user_id: "u", channel: "email", title: "t", message: huge });
    expect(res.status).toBe(413);
    expect(res.body.error).toBe("request body too large");
  });

  it("accepts a small JSON POST under the limit (201)", async () => {
    const res = await request(app)
      .post("/api/notifications/send")
      .send({ user_id: "u", channel: "email", title: "t", message: "m" });
    expect(res.status).toBe(201);
  });
});

describe("Malformed JSON body", () => {
  it("returns 400 with JSON error on syntactically invalid JSON", async () => {
    const res = await request(app)
      .post("/api/notifications/send")
      .set("Content-Type", "application/json")
      .send("{not-json");
    expect(res.status).toBe(400);
    expect(res.headers["content-type"]).toMatch(/application\/json/);
    expect(res.body.error).toBe("invalid JSON body");
  });

  it("returns 400 on JSON with trailing comma", async () => {
    const res = await request(app)
      .post("/api/notifications/send")
      .set("Content-Type", "application/json")
      .send('{"user_id":"u","channel":"email",}');
    expect(res.status).toBe(400);
    expect(res.body.error).toBe("invalid JSON body");
  });

  it("returns 400 with JSON content-type but non-JSON body", async () => {
    const res = await request(app)
      .post("/api/notifications/send")
      .set("Content-Type", "application/json")
      .send("plain text body");
    expect(res.status).toBe(400);
    expect(res.body.error).toBe("invalid JSON body");
  });

  it("does not affect normal JSON parsing (valid body still routes through)", async () => {
    const res = await request(app)
      .post("/api/notifications/send")
      .send({ user_id: "u2", channel: "email", title: "t", message: "m" });
    expect(res.status).toBe(201);
  });
});

describe("DELETE /api/notifications", () => {
  async function seedNotification(
    user_id: string,
    channel: "email" | "sms" | "push",
    title = "t",
    message = "m",
  ): Promise<string> {
    const res = await request(app)
      .post("/api/notifications/send")
      .send({ user_id, channel, title, message });
    return res.body.id as string;
  }

  it("requires at least one filter", async () => {
    await seedNotification("u1", "email");
    const res = await request(app).delete("/api/notifications");
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("at least one of");
    // 通知は削除されていないこと
    const list = await request(app).get("/api/notifications");
    expect(list.body).toHaveLength(1);
  });

  it("deletes by user_id", async () => {
    await seedNotification("u1", "email");
    await seedNotification("u1", "sms");
    await seedNotification("u2", "email");
    const res = await request(app).delete("/api/notifications?user_id=u1");
    expect(res.status).toBe(200);
    expect(res.body.deleted).toBe(2);
    expect(res.body.user_id).toBe("u1");
    expect(res.body.channel).toBeNull();
    expect(res.body.status).toBeNull();

    const list = await request(app).get("/api/notifications");
    expect(list.body).toHaveLength(1);
    expect(list.body[0].user_id).toBe("u2");
  });

  it("deletes by channel", async () => {
    await seedNotification("u1", "email");
    await seedNotification("u1", "sms");
    await seedNotification("u2", "email");
    const res = await request(app).delete("/api/notifications?channel=email");
    expect(res.status).toBe(200);
    expect(res.body.deleted).toBe(2);
    expect(res.body.channel).toBe("email");

    const list = await request(app).get("/api/notifications");
    expect(list.body).toHaveLength(1);
    expect(list.body[0].channel).toBe("sms");
  });

  it("deletes by status", async () => {
    // 全 POST は status='sent' で作成されるため、status=failed では削除 0 件
    await seedNotification("u1", "email");
    const noMatch = await request(app).delete(
      "/api/notifications?status=failed",
    );
    expect(noMatch.status).toBe(200);
    expect(noMatch.body.deleted).toBe(0);

    const match = await request(app).delete("/api/notifications?status=sent");
    expect(match.status).toBe(200);
    expect(match.body.deleted).toBe(1);
    expect(match.body.status).toBe("sent");
  });

  it("combines filters with AND semantics", async () => {
    await seedNotification("u1", "email");
    await seedNotification("u1", "sms");
    await seedNotification("u2", "email");
    // user_id=u1 AND channel=email → 1 件のみ削除
    const res = await request(app).delete(
      "/api/notifications?user_id=u1&channel=email",
    );
    expect(res.status).toBe(200);
    expect(res.body.deleted).toBe(1);

    const list = await request(app).get("/api/notifications");
    expect(list.body).toHaveLength(2);
    // u1+sms と u2+email が残る
    expect(list.body.some(
      (n: { user_id: string; channel: string }) =>
        n.user_id === "u1" && n.channel === "sms",
    )).toBe(true);
    expect(list.body.some(
      (n: { user_id: string; channel: string }) =>
        n.user_id === "u2" && n.channel === "email",
    )).toBe(true);
  });

  it("returns 400 for invalid channel", async () => {
    const res = await request(app).delete(
      "/api/notifications?channel=telegram",
    );
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("channel must be one of");
  });

  it("returns 400 for invalid status", async () => {
    const res = await request(app).delete(
      "/api/notifications?status=expired",
    );
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("status must be one of");
  });

  it("returns 400 for invalid since (not ISO 8601)", async () => {
    const res = await request(app).delete(
      "/api/notifications?since=not-a-date",
    );
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("since");
  });

  it("returns 400 when since > until", async () => {
    const res = await request(app).delete(
      "/api/notifications?since=2026-01-02T00:00:00Z&until=2026-01-01T00:00:00Z",
    );
    expect(res.status).toBe(400);
    expect(res.body.error).toContain(
      "until must be greater than or equal to since",
    );
  });

  it("returns deleted=0 when no notifications match the filter", async () => {
    await seedNotification("u1", "email");
    const res = await request(app).delete(
      "/api/notifications?user_id=unknown",
    );
    expect(res.status).toBe(200);
    expect(res.body.deleted).toBe(0);
    expect(res.body.user_id).toBe("unknown");

    const list = await request(app).get("/api/notifications");
    expect(list.body).toHaveLength(1);
  });

  it("does not delete notifications that match nothing in a multi-filter request", async () => {
    await seedNotification("u1", "email");
    await seedNotification("u1", "sms");
    // user_id=u1 AND channel=push → どれも一致せず deleted=0、両方残る
    const res = await request(app).delete(
      "/api/notifications?user_id=u1&channel=push",
    );
    expect(res.status).toBe(200);
    expect(res.body.deleted).toBe(0);

    const list = await request(app).get("/api/notifications");
    expect(list.body).toHaveLength(2);
  });

  it("filters by since (inclusive lower bound)", async () => {
    const id1 = await seedNotification("u1", "email");
    // 少し待ってから 2 件目を作成
    await new Promise((r) => setTimeout(r, 20));
    const id2 = await seedNotification("u1", "email");

    // id2 の created_at を since に指定 → id1 は対象外、id2 のみ削除
    const get2 = await request(app).get(`/api/notifications/${id2}`);
    const since = get2.body.created_at as string;
    const res = await request(app).delete(
      `/api/notifications?since=${encodeURIComponent(since)}`,
    );
    expect(res.status).toBe(200);
    expect(res.body.deleted).toBe(1);
    const remain = await request(app).get(`/api/notifications/${id1}`);
    expect(remain.status).toBe(200);
  });
});
