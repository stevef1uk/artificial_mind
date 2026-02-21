// MyClimate Flight Calculator - Direct Scraper Script
// This script handles the MyClimate flight calculator form with proper element discovery

await page.goto('https://co2.myclimate.org/en/flight_calculators/new', { waitUntil: 'networkidle' });

// Wait for form to fully initialize
await page.waitForTimeout(2000);

// Find and fill "From" field by searching for input inside label containing "From"
const fromLabel = await page.locator('label:has-text("From")').locator('..').locator('input').first();
if (fromLabel) {
  await fromLabel.fill('CDG');
  await page.waitForTimeout(1500);
  // Click first dropdown option
  const fromOption = await page.locator('ul >> li >> text=Charles').first();
  if (fromOption) await fromOption.click();
  await page.waitForTimeout(500);
}

// Find and fill "To" field
const toLabel = await page.locator('label:has-text("To")').locator('..').locator('input').first();
if (toLabel) {
  await toLabel.fill('LHR');
  await page.waitForTimeout(1500);
  const toOption = await page.locator('ul >> li >> text=London').first();
  if (toOption) await toOption.click();
  await page.waitForTimeout(500);
}

// Select Aircraft - try finding select by aria-label or data-* attributes
await page.selectOption('select[name*="aircraft"], select[aria-label*="aircraft"]', 'BOEING_737');

// Select Passengers
await page.selectOption('select[name*="passenger"], select[aria-label*="passenger"]', '1');

// Select Class (Economy)
const economyRadio = await page.locator('input[type="radio"][value*="economy"], label:has-text("Economy")');
if (economyRadio) await economyRadio.check();

// Click Calculate button
const calcButton = await page.locator('button:has-text("Calculate"), button:has-text("Submit"), button[type="submit"]').first();
if (calcButton) {
  await calcButton.click();
  await page.waitForTimeout(3000);
}

// Extract results
const distance = await page.locator('[class*="distance"], [id*="distance"], text=/\\d+ km/').first().textContent();
const emissions = await page.locator('[class*="emissions"], [class*="co2"], [id*="co2"], text=/\\d+(\\.\\d+)? [kt]g/').first().textContent();
