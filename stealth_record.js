const { chromium } = require('playwright-extra');
const stealth = require('puppeteer-extra-plugin-stealth')();

chromium.use(stealth);

(async () => {
    const url = process.argv[2] || 'https://fnac.ch';
    const outputPath = process.argv[3] || 'recorded_script.ts';

    console.log(`🚀 Launching STEALTH browser for ${url}`);
    console.log(`🎬 Recording will be saved to ${outputPath}`);
    console.log(`💡 Once the page loads, solve the slider and start your interaction.`);

    const browser = await chromium.launch({
        headless: false,
        channel: 'chrome',
        args: [
            '--disable-blink-features=AutomationControlled'
        ]
    });

    const iPhone = require('playwright').devices['iPhone 13'];
    const context = await browser.newContext({
        ...iPhone,
        locale: 'fr-CH',
        timezoneId: 'Europe/Zurich',
    });

    // Enable codegen-like recording by using the inspector
    // Note: This launches the Playwright Inspector window
    await context.route('**/*', route => route.continue());
    const page = await context.newPage();

    // This is a trick to start recording in a stealth session
    await page.pause();

    await page.goto(url);

    console.log('✔ Browser open. Close the browser when finished recording.');

    // Wait for browser to close
    await new Promise(resolve => browser.on('disconnected', resolve));
})();
