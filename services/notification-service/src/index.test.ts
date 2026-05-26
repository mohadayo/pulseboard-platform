import request from "supertest";
import { app, clearNotifications } from "./index";

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
