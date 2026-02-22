import { chromium } from 'playwright';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const fixtureURL = pathToFileURL(path.resolve('.tmp/e2e/markdown-list.html')).toString();
const screenshotPath = path.resolve('.tmp/e2e/markdown-list-e2e.png');

const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  await page.goto(fixtureURL);
  await page.waitForSelector('#ordered ol > li');
  await page.waitForSelector('#unordered ul > li');

  const ordered = await page.evaluate(() => {
    const olList = Array.from(document.querySelectorAll('#ordered ol'));
    const li = Array.from(document.querySelectorAll('#ordered ol > li'));
    return {
      olCount: olList.length,
      liCount: li.length,
      values: li.map((node) => Number(node.value)),
      texts: li.map((node) => (node.textContent || '').trim()),
    };
  });

  const unordered = await page.evaluate(() => {
    const ulList = Array.from(document.querySelectorAll('#unordered ul'));
    const li = Array.from(document.querySelectorAll('#unordered ul > li'));
    return {
      ulCount: ulList.length,
      liCount: li.length,
      texts: li.map((node) => (node.textContent || '').trim()),
    };
  });

  await page.screenshot({ path: screenshotPath, fullPage: true });

  const orderedOK = ordered.olCount === 1
    && ordered.liCount === 3
    && ordered.values.join(',') === '1,2,3';
  const unorderedOK = unordered.ulCount === 1 && unordered.liCount === 3;

  const result = {
    fixtureURL,
    screenshotPath,
    ordered,
    unordered,
    pass: orderedOK && unorderedOK,
  };

  console.log(JSON.stringify(result, null, 2));

  if (!result.pass) {
    process.exitCode = 1;
  }
} finally {
  await browser.close();
}
