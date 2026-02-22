import { expect, test } from "@playwright/test";

test("sidebar navigation switches between chat and settings", async ({ page }) => {
    await page.goto("/");

    await expect(page.getByTestId("page-chat")).toBeVisible();
    await page.getByTestId("nav-settings").click();
    await expect(page.getByTestId("page-settings")).toBeVisible();

    await page.getByTestId("nav-chat").click();
    await expect(page.getByTestId("page-chat")).toBeVisible();
});

test("archive toggle updates empty state text", async ({ page }) => {
    await page.goto("/");

    await expect(page.getByTestId("thread-empty-state")).toContainText("暂无会话");
    await page.getByTestId("thread-archive-toggle").click();
    await expect(page.getByTestId("thread-empty-state")).toContainText("暂无归档会话");
    await page.getByTestId("thread-archive-toggle").click();
    await expect(page.getByTestId("thread-empty-state")).toContainText("暂无会话");
});

test("composer is disabled without selected thread", async ({ page }) => {
    await page.goto("/");

    await expect(page.getByTestId("composer-input")).toBeDisabled();
    await expect(page.getByTestId("composer-attach-button")).toBeDisabled();
    await expect(page.getByTestId("composer-compact-button")).toBeDisabled();
    await expect(page.getByTestId("composer-send-button")).toBeDisabled();
});
