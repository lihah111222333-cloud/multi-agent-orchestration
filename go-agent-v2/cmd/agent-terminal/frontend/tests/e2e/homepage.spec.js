import { expect, test } from "@playwright/test";

test("frontend chat shell renders empty-state controls", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("button", { name: "启动 Agent" })).toBeVisible();
    await expect(page.getByText("暂无会话，点击顶部「启动 Agent」开始对话")).toBeVisible();
});
