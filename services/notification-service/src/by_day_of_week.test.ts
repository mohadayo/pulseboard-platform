import request from "supertest";
import { app, clearNotifications } from "./index";

// notification-service の `GET /api/notifications/by_day_of_week` の回帰テスト。
// `index.test.ts` の `describe("GET /api/notifications/by_hour_of_day", ...)` と
// 対称な構造を保ちつつ、`describe("GET /api/notifications/by_day", ...)` と同じ
// クエリバリデーション・ルーティング衝突回避の観点で網羅する。
// POST 経由の created_at は `new Date().toISOString()` で書き込まれるため、
// 曜日単位の細かい分岐（月〜日 7 曜日全部にそれぞれ 1 件ずつ入れる 等）は
// テストが実行される曜日に依存して不安定になる。したがってこのファイルでは、
// 総件数 / distinct_days_of_week / lex 昇順 / ラベル網羅性 / フィルタの効き /
// 400 応答 / /:id 衝突回避 という「曜日値そのもの」に依存しない性質を検証する。

beforeEach(() => {
  clearNotifications();
});

describe("GET /api/notifications/by_day_of_week", () => {
  const send = (
    channel: "email" | "sms" | "push" = "email",
    user_id = "u1",
  ) =>
    request(app).post("/api/notifications/send").send({
      user_id,
      channel,
      title: "T",
      message: "M",
    });

  it("returns empty aggregation when store is empty", async () => {
    const res = await request(app).get("/api/notifications/by_day_of_week");
    expect(res.status).toBe(200);
    expect(res.body).toEqual({
      total: 0,
      distinct_days_of_week: 0,
      by_day_of_week: [],
    });
  });

  it("groups notifications by UTC ISO weekday and preserves totals", async () => {
    // 同一プロセスで連続 POST するため、通常は 1 曜日にのみ落ちるはず。
    // ただしテスト実行が UTC 深夜 (23:59:xx) を跨ぐと 2 曜日にまたがりうるため
    // `<= 2` で緩めて回帰安定性を確保する。
    await send();
    await send();
    await send();
    const res = await request(app).get("/api/notifications/by_day_of_week");
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(3);
    expect(res.body.distinct_days_of_week).toBeGreaterThanOrEqual(1);
    expect(res.body.distinct_days_of_week).toBeLessThanOrEqual(2);
    expect(res.body.by_day_of_week.length).toBe(
      res.body.distinct_days_of_week,
    );
    const summedCount = res.body.by_day_of_week.reduce(
      (acc: number, row: { count: number }) => acc + row.count,
      0,
    );
    expect(summedCount).toBe(3);
  });

  it("returns day keys as \"1\"-\"7\" with matching weekday_name labels", async () => {
    await send();
    await send();
    const res = await request(app).get("/api/notifications/by_day_of_week");
    expect(res.status).toBe(200);
    const validNames: Record<string, string> = {
      "1": "Mon",
      "2": "Tue",
      "3": "Wed",
      "4": "Thu",
      "5": "Fri",
      "6": "Sat",
      "7": "Sun",
    };
    for (const row of res.body.by_day_of_week) {
      // ISO 曜日番号 "1"..."7" の 1 文字。
      expect(row.day).toMatch(/^[1-7]$/);
      // day と weekday_name の対応が固定表に一致する。
      expect(row.weekday_name).toBe(validNames[row.day]);
    }
  });

  it("returns keys in lex ascending order (= Mon..Sun order)", async () => {
    await send();
    await send();
    const res = await request(app).get("/api/notifications/by_day_of_week");
    expect(res.status).toBe(200);
    const days = res.body.by_day_of_week.map((row: { day: string }) => row.day);
    const sorted = [...days].sort();
    expect(days).toEqual(sorted);
  });

  it("filters by user_id", async () => {
    await send("email", "u1");
    await send("email", "u1");
    await send("sms", "u2");
    const res = await request(app).get(
      "/api/notifications/by_day_of_week?user_id=u1",
    );
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(2);
  });

  it("filters by channel", async () => {
    await send("email");
    await send("sms");
    await send("push");
    const res = await request(app).get(
      "/api/notifications/by_day_of_week?channel=email",
    );
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(1);
  });

  it("filters by status", async () => {
    // POST は常に status=sent で作成されるため、status=sent なら 2 件、status=failed なら 0 件。
    await send("email");
    await send("sms");
    const sentRes = await request(app).get(
      "/api/notifications/by_day_of_week?status=sent",
    );
    expect(sentRes.status).toBe(200);
    expect(sentRes.body.total).toBe(2);
    const failedRes = await request(app).get(
      "/api/notifications/by_day_of_week?status=failed",
    );
    expect(failedRes.status).toBe(200);
    expect(failedRes.body.total).toBe(0);
    expect(failedRes.body.distinct_days_of_week).toBe(0);
    expect(failedRes.body.by_day_of_week).toEqual([]);
  });

  it("respects since/until filters", async () => {
    await send();
    await new Promise((r) => setTimeout(r, 10));
    const cutoff = new Date(Date.now()).toISOString();
    await new Promise((r) => setTimeout(r, 10));
    await send();
    await send();
    const res = await request(app).get(
      `/api/notifications/by_day_of_week?since=${encodeURIComponent(cutoff)}`,
    );
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(2);
  });

  it("rejects invalid channel with 400", async () => {
    const res = await request(app).get(
      "/api/notifications/by_day_of_week?channel=bogus",
    );
    expect(res.status).toBe(400);
  });

  it("rejects invalid status with 400", async () => {
    const res = await request(app).get(
      "/api/notifications/by_day_of_week?status=bogus",
    );
    expect(res.status).toBe(400);
  });

  it("rejects invalid since with 400", async () => {
    const res = await request(app).get(
      "/api/notifications/by_day_of_week?since=not-a-date",
    );
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("since");
  });

  it("rejects since > until with 400", async () => {
    const res = await request(app).get(
      "/api/notifications/by_day_of_week?since=2026-01-01T00:00:00Z&until=2024-01-01T00:00:00Z",
    );
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("until");
  });

  it("does not collide with /:id lookup route", async () => {
    // "/api/notifications/by_day_of_week" は上位で by_day_of_week handler に
    // マッチし、404 になる /:id handler に落ちないことを確認する（登録順序の回帰防止）。
    const res = await request(app).get("/api/notifications/by_day_of_week");
    expect(res.status).toBe(200);
    expect(res.body).toHaveProperty("by_day_of_week");
    expect(res.body).not.toHaveProperty("error");
  });
});
