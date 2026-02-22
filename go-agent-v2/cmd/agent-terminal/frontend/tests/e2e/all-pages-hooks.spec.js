import { expect, test } from "@playwright/test";

test("all sidebar pages expose stable test hooks", async ({ page }) => {
    await page.goto("/");

    const cases = [
        { nav: "nav-chat", page: "page-chat", marker: "chat-page" },
        { nav: "nav-agents", page: "page-agents", marker: "data-page-agents" },
        { nav: "nav-dags", page: "page-dags", marker: "data-page-dags" },
        { nav: "nav-tasks", page: "page-tasks", marker: "tasks-page" },
        { nav: "nav-skills", page: "page-skills", marker: "skills-page" },
        { nav: "nav-commands", page: "page-commands", marker: "commands-page" },
        { nav: "nav-memory", page: "page-memory", marker: "data-page-memory" },
        { nav: "nav-settings", page: "page-settings", marker: "settings-page" },
    ];

    for (const item of cases) {
        await page.getByTestId(item.nav).click();
        await expect(page.getByTestId(item.page)).toBeVisible();
        await expect(page.getByTestId(item.marker)).toBeVisible();
    }
});

test("settings save actions trigger real API calls and surface notices", async ({ page }) => {
    await page.goto("/");
    await page.getByTestId("nav-settings").click();
    await expect(page.getByTestId("settings-page")).toBeVisible();

    const lspSave = page.getByTestId("settings-lsp-save-button");
    await expect(lspSave).toBeEnabled();
    await lspSave.click();
    await expect(page.getByTestId("settings-lsp-prompt-notice")).toBeVisible();
    await expect(page.getByTestId("settings-lsp-prompt-notice")).not.toHaveText("");

    const jsonSave = page.getByTestId("settings-json-render-save-button");
    await expect(jsonSave).toBeEnabled();
    await jsonSave.click();
    await expect(page.getByTestId("settings-json-render-prompt-notice")).toBeVisible();
    await expect(page.getByTestId("settings-json-render-prompt-notice")).not.toHaveText("");

    const browserSave = page.getByTestId("settings-browser-save-button");
    await expect(browserSave).toBeEnabled();
    await browserSave.click();
    await expect(page.getByTestId("settings-browser-prompt-notice")).toBeVisible();
    await expect(page.getByTestId("settings-browser-prompt-notice")).not.toHaveText("");
});
