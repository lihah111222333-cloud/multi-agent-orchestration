const { test, expect } = require('@playwright/test');
const path = require('node:path');
const { pathToFileURL } = require('node:url');

test('ordered list keeps one ol and increments numbers across blank lines', async ({ page }) => {
  const fixture = pathToFileURL(path.resolve(__dirname, 'markdown-list.html')).toString();
  await page.goto(fixture);

  const orderedRoot = page.locator('#ordered');
  await expect(orderedRoot.locator('ol')).toHaveCount(1);
  await expect(orderedRoot.locator('ol > li')).toHaveCount(3);

  const values = await orderedRoot.locator('ol > li').evaluateAll((nodes) =>
    nodes.map((node) => Number(node.value))
  );
  expect(values).toEqual([1, 2, 3]);
});

test('unordered list keeps one ul across blank lines', async ({ page }) => {
  const fixture = pathToFileURL(path.resolve(__dirname, 'markdown-list.html')).toString();
  await page.goto(fixture);

  const unorderedRoot = page.locator('#unordered');
  await expect(unorderedRoot.locator('ul')).toHaveCount(1);
  await expect(unorderedRoot.locator('ul > li')).toHaveCount(3);
});
