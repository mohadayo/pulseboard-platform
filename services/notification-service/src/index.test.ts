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
