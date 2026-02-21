const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  
  console.log('📍 Loading MyClimate form...');
  await page.goto('https://co2.myclimate.org/en/flight_calculators/new', { waitUntil: 'networkidle' });
  
  console.log('📝 Filling form fields...');
  
  // Fill the "From" airport
  await page.fill('input[id="flight_calculator_from"]', 'CDG');
  await page.waitForTimeout(500);
  
  // Fill the "To" airport  
  await page.fill('input[id="flight_calculator_to"]', 'LHR');
  await page.waitForTimeout(500);
  
  // Select aircraft type
  await page.selectOption('select[id="flight_calculator_aircraft_type_leg_1"]', 'BOEING_737');
  
  // Select number of passengers (already defaulted to 1)
  await page.selectOption('select[id="flight_calculator_passengers"]', '1');
  
  // Click "One way" radio button 
  const oneWayRadio = 'input[id="flight_calculator_roundtrip_false"]';
  const isReadonly = await page.getAttribute(oneWayRadio, 'readonly');
  if (isReadonly) {
    console.log('⚠️ One-way radio is readonly, checking if it matters...');
    // Try clicking the direct flight radio instead
    await page.click('input[id="flight_calculator_control_via_fields_direct"]');
  } else {
    await page.click(oneWayRadio);
  }
  
  // Economy class should already be selected, so just submit
  console.log('🔘 Clicking submit button...');
  await page.click('input[type="submit"]');
  
  // Wait for results to load - try multiple selectors
  console.log('⏳ Waiting for results...');
  try {
    await Promise.race([
      page.waitForSelector('div.calculator-result', { timeout: 5000 }),
      page.waitForSelector('.results', { timeout: 5000 }),
      page.waitForSelector('[class*="result"]', { timeout: 5000 }),
    ]).catch(() => {
      console.log('⏱️ No standard result selector found, checking page state...');
    });
  } catch (e) {
    console.log('Error waiting:', e.message);
  }
  
  await page.waitForTimeout(2000); // Extra wait for JS processing
  
  console.log('📄 Current URL:', page.url());
  console.log('📄 Page title:', await page.title());
  
  // Get page content
  const content = await page.content();
  
  // Search for known result patterns
  if (content.includes('CO₂ emissions') || content.includes('emissions')) {
    console.log('✅ Found "emissions" in page');
  }
  if (content.includes('distance') || content.includes('Distance')) {
    console.log('✅ Found "distance" in page');
  }
  if (content.includes('km')) {
    console.log('✅ Found "km" in page');
  }
  
  // Try to find the actual result div/section
  const resultSection = await page.evaluate(() => {
    const possible = [
      document.querySelector('.calculator-result'),
      document.querySelector('[class*="result"]'),
      document.querySelector('main'),
      document.querySelector('[role="main"]'),
    ];
    
    for (const elem of possible) {
      if (elem && elem.textContent.includes('emissions')) {
        return {
          className: elem.className,
          tagName: elem.tagName,
          textSnippet: elem.textContent.substring(0, 500),
        };
      }
    }
    return null;
  });
  
  if (resultSection) {
    console.log('🎯 Found result section:', resultSection);
  }
  
  // Save a screenshot for debugging
  await page.screenshot({ path: '/tmp/myclimate-result.png' });
  console.log('📸 Screenshot saved to /tmp/myclimate-result.png');
  
  // Save HTML snippet for analysis
  const mainHtml = await page.evaluate(() => {
    const main = document.querySelector('main');
    return main ? main.innerHTML.substring(0, 2000) : 'No main found';
  });
  
  console.log('\n📋 HTML snippet from main:');
  console.log(mainHtml);
  
  await browser.close();
});
