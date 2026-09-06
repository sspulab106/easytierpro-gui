"""Verify the topology view renders on the dashboard."""
import sys
from playwright.sync_api import sync_playwright

from common import base_url

BASE = base_url()


def main():
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page(viewport={"width": 1400, "height": 900})
        page.goto(BASE + "/", wait_until="networkidle", timeout=30000)
        page.wait_for_selector("main", timeout=20000)
        page.wait_for_timeout(1500)

        # Start core if not running
        start = page.locator("button:has-text('Start Core')")
        if start.count() > 0:
            start.click()
            for _ in range(40):
                page.wait_for_timeout(1000)
                if "running" in page.locator(".badge").first.inner_text().lower():
                    break

        # Wait for topology SVG with LOCAL label
        found = False
        for _ in range(20):
            page.wait_for_timeout(1500)
            main_text = page.evaluate("document.querySelector('main')?.innerText || ''")
            if "Network Topology" in main_text and "LOCAL" in main_text:
                found = True
                break
        page.screenshot(path="C:/Users/admin/AppData/Local/Temp/opencode/topology.png", full_page=True)
        print("PASS topology-renders" if found else "FAIL topology-renders")
        sys.exit(0 if found else 1)


if __name__ == "__main__":
    main()

