import { test, expect } from '@playwright/test';
test('test', async ({ page }) => {
  await page.goto('https://www.amazon.fr/-/en/gp/product/B0G1CC2949');
  await page.click('#sp-cc-accept');
  await page.waitForSelector('div#tp-tool-tip-price-block > div > div');
});
