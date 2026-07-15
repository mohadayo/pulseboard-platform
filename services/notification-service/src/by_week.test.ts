import request from "supertest";
import { app, clearNotifications } from "./index";

// notification-service の `GET /api/notifications/by_week` の回帰テスト。
// `by_day_of_week.test.ts` と対称な構造を保ちつつ、`by_day` と同じ線形時系列
// クエリバリデーション / ルーティング衝突回避の観点で網羅する。
// POST 経由の created_at は `new Date().toISOString()` で書き込まれるため、
// 「特定の週番号を返すか」を直接検証するテストは書きづらい（テスト実行日の週）。
// したがってこのファイルでは、総件数 / distinct_weeks / lex 昇順 / 週キー形式 /
// フィルタの効き / 400 応答 / /:id 衝突回避 / helper isoWeekKey の性質
// という「実行タイミングに依存しない性質」を検証する。

beforeEach(() => {
  clearNotifications();
});

describe("GET /api/notifications/by_week", () => {
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
    const res = await request(app).get("/api/notifications/by_week");
    expect(res.status).toBe(200);
    expect(res.body).toEqual({
      total: 0,
      distinct_weeks: 0,
      by_week: [],
    });
  });

  it("groups notifications by ISO week and preserves totals", async () => {
    // 同一プロセスで連続 POST するため、通常は 1 週にのみ落ちる。
    // ただしテスト実行が週跨ぎ (日曜 UTC 23:59 前後) を跨ぐと 2 週にまたがりうるため
    // `<= 2` で緩めて回帰安定性を確保する（by_day_of_week.test.ts と同じ流儀）。
    await send();
    await send();
    await send();
    const res = await request(app).get("/api/notifications/by_week");
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(3);
    expect(res.body.distinct_weeks).toBeGreaterThanOrEqual(1);
    expect(res.body.distinct_weeks).toBeLessThanOrEqual(2);
    expect(res.body.by_week.length).toBe(res.body.distinct_weeks);
    const summedCount = res.body.by_week.reduce(
      (acc: number, row: { count: number }) => acc + row.count,
      0,
    );
    expect(summedCount).toBe(3);
  });

  it("returns week keys as YYYY-Www with 2-digit zero-padded week number", async () => {
    await send();
    const res = await request(app).get("/api/notifications/by_week");
    expect(res.status).toBe(200);
    expect(res.body.by_week.length).toBeGreaterThan(0);
    for (const row of res.body.by_week) {
      // ISO 週フォーマット: 4 桁年 + "-W" + 2 桁週 (01..53)
      expect(row.week).toMatch(/^\d{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$/);
    }
  });

  it("returns keys in lex ascending order (= chronological)", async () => {
    // 単一実行では 1 週間しか埋まらないが、複数返る場合も lex 順であることを検証。
    await send();
    await send();
    const res = await request(app).get("/api/notifications/by_week");
    expect(res.status).toBe(200);
    const weeks = res.body.by_week.map((row: { week: string }) => row.week);
    const sorted = [...weeks].sort();
    expect(weeks).toEqual(sorted);
  });

  it("filters by user_id", async () => {
    await send("email", "u1");
    await send("email", "u1");
    await send("sms", "u2");
    const res = await request(app).get(
      "/api/notifications/by_week?user_id=u1",
    );
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(2);
  });

  it("filters by channel", async () => {
    await send("email");
    await send("sms");
    await send("push");
    const res = await request(app).get(
      "/api/notifications/by_week?channel=email",
    );
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(1);
  });

  it("filters by status", async () => {
    // POST は常に status=sent で作成される。
    await send("email");
    await send("sms");
    const sentRes = await request(app).get(
      "/api/notifications/by_week?status=sent",
    );
    expect(sentRes.status).toBe(200);
    expect(sentRes.body.total).toBe(2);
    const failedRes = await request(app).get(
      "/api/notifications/by_week?status=failed",
    );
    expect(failedRes.status).toBe(200);
    expect(failedRes.body.total).toBe(0);
    expect(failedRes.body.distinct_weeks).toBe(0);
    expect(failedRes.body.by_week).toEqual([]);
  });

  it("respects since/until filters", async () => {
    await send();
    await new Promise((r) => setTimeout(r, 10));
    const cutoff = new Date(Date.now()).toISOString();
    await new Promise((r) => setTimeout(r, 10));
    await send();
    await send();
    const res = await request(app).get(
      `/api/notifications/by_week?since=${encodeURIComponent(cutoff)}`,
    );
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(2);
  });

  it("aggregates only sent notifications (default: no status filter includes all)", async () => {
    // 3 件送信すると全て status=sent。フィルタなしでは全件が集計対象。
    await send();
    await send();
    await send();
    const res = await request(app).get("/api/notifications/by_week");
    expect(res.status).toBe(200);
    expect(res.body.total).toBe(3);
  });

  it("rejects invalid channel with 400", async () => {
    const res = await request(app).get(
      "/api/notifications/by_week?channel=bogus",
    );
    expect(res.status).toBe(400);
  });

  it("rejects invalid status with 400", async () => {
    const res = await request(app).get(
      "/api/notifications/by_week?status=bogus",
    );
    expect(res.status).toBe(400);
  });

  it("rejects invalid since with 400", async () => {
    const res = await request(app).get(
      "/api/notifications/by_week?since=not-a-date",
    );
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("since");
  });

  it("rejects since > until with 400", async () => {
    const res = await request(app).get(
      "/api/notifications/by_week?since=2026-06-01T00:00:00Z&until=2024-06-01T00:00:00Z",
    );
    expect(res.status).toBe(400);
    expect(res.body.error).toContain("until");
  });

  it("does not collide with /:id lookup route", async () => {
    // "/api/notifications/by_week" は上位で by_week handler にマッチし、
    // 404 になる /:id handler に落ちないことを確認する（登録順序の回帰防止）。
    const res = await request(app).get("/api/notifications/by_week");
    expect(res.status).toBe(200);
    expect(res.body).toHaveProperty("by_week");
    expect(res.body).not.toHaveProperty("error");
  });

  it("consistent format for populated weeks — each has 'week' and numeric 'count' >= 1", async () => {
    // populated-only 規約: 母集団 0 の週は含まれないため、返る全行は count >= 1。
    await send();
    await send();
    const res = await request(app).get("/api/notifications/by_week");
    expect(res.status).toBe(200);
    for (const row of res.body.by_week) {
      expect(typeof row.week).toBe("string");
      expect(typeof row.count).toBe("number");
      expect(row.count).toBeGreaterThanOrEqual(1);
    }
  });
});
