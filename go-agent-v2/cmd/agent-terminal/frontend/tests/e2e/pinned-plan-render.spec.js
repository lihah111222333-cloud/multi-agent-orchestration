import { expect, test } from "@playwright/test";

const MOCK_RUNTIME_MODULE = `
const METHOD_IDS = Object.freeze({
  CALL_API: 1055257995,
  GET_BUILD_INFO: 3168473285,
});

const snapshot = Object.freeze({
  threads: [
    { id: "thread-plan-1", name: "Plan Render Thread", state: "running" },
  ],
  statuses: {
    "thread-plan-1": "running",
  },
  timelinesByThread: {
    "thread-plan-1": [
      {
        id: "msg-user-1",
        kind: "user",
        text: "请给出一个执行计划",
        ts: "2026-02-22T10:00:00.000Z",
      },
      {
        id: "msg-plan-1",
        kind: "plan",
        done: false,
        text: "1. 收集上下文\\n2. 实施修复\\n3. 回归验证",
        ts: "2026-02-22T10:00:02.000Z",
      },
      {
        id: "msg-assistant-1",
        kind: "assistant",
        text: "收到，开始执行。",
        ts: "2026-02-22T10:00:03.000Z",
      },
    ],
  },
  activeThreadId: "thread-plan-1",
  activeCmdThreadId: "",
  mainAgentId: "thread-plan-1",
  "viewPrefs.chat": { layout: "mix", splitRatio: 60 },
  "viewPrefs.cmd": { layout: "mix", splitRatio: 60, cardCols: 3 },
  "threadPins.chat": {},
  "threadArchives.chat": {},
});

function clone(value) {
  return JSON.parse(JSON.stringify(value));
}

function callAPI(method, params) {
  if (method === "thread/list") return { threads: clone(snapshot.threads) };
  if (method === "ui/state/get") return clone(snapshot);
  if (method === "thread/messages") {
    const threadId = (params && params.threadId) ? String(params.threadId) : "";
    return { messages: clone((snapshot.timelinesByThread && snapshot.timelinesByThread[threadId]) || []) };
  }
  if (method === "ui/dashboard/get") return {};
  if (method === "ui/preferences/set") return { ok: true };
  if (method === "thread/name/set") return { ok: true };
  if (method === "thread/resolve") return {};
  if (method === "ui/copyText") return { ok: true };
  if (method === "turn/interrupt") return { confirmed: false, interruptSent: false, mode: "no_active_turn" };
  return {};
}

export const Call = {
  async ByID(methodId, ...args) {
    if (methodId === METHOD_IDS.GET_BUILD_INFO) {
      return { version: "test", commit: "mock", builtAt: "2026-02-22T00:00:00.000Z" };
    }
    if (methodId === METHOD_IDS.CALL_API) {
      const method = (args[0] || "").toString();
      const params = args[1] && typeof args[1] === "object" ? args[1] : {};
      return callAPI(method, params);
    }
    return {};
  },
};

const listeners = new Map();

export const Events = {
  On(name, callback) {
    listeners.set(name, callback);
    return () => listeners.delete(name);
  },
  Off(name) {
    listeners.delete(name);
  },
};
`;

test("pinned plan renders top-right and does not duplicate in timeline", async ({ page }) => {
  await page.route("**/wails/runtime.js", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/javascript",
      body: MOCK_RUNTIME_MODULE,
    });
  });

  await page.goto("/");
  await expect(page.getByTestId("chat-page")).toBeVisible();

  const pinnedPlan = page.locator(".chat-plan-pin");
  await expect(pinnedPlan).toBeVisible();
  await expect(pinnedPlan).toContainText("PLAN");
  await expect(pinnedPlan).toContainText("进行中");
  await expect(page.locator(".chat-plan-pin-close")).toBeVisible();

  await expect(page.locator(".chat-item.kind-plan.process")).toHaveCount(0);
  await expect(page.locator(".chat-item.kind-user.dialog")).toHaveCount(1);
  await expect(page.locator(".chat-item.kind-assistant.dialog")).toHaveCount(1);

  await page.screenshot({ path: "test-results/pinned-plan-render-pass.png", fullPage: true });
});
