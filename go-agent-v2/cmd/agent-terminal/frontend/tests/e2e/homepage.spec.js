import { expect, test } from "@playwright/test";

test("frontend chat shell renders empty-state controls", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByTestId("app-shell")).toBeVisible();
    await expect(page.getByTestId("chat-page")).toBeVisible();
    await expect(page.getByTestId("launch-agent-button")).toBeVisible();
    await expect(page.getByTestId("thread-empty-state")).toContainText("暂无会话");
    await expect(page.getByTestId("chat-empty-state")).toContainText("选择或启动一个 Agent 开始对话");
});
