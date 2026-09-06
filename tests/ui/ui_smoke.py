"""Automated UI smoke test for the EasyTier Pro web management console."""
import sys
import json
import time

from playwright.sync_api import sync_playwright

from common import base_url

BASE = base_url()

def main():
    results = []
    def check(name, ok, detail=""):
        results.append((name, ok, detail))
        print(("PASS" if ok else "FAIL"), name, detail)

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page(viewport={"width": 1280, "height": 800})

        console_errors = []
        page.on("console", lambda m: console_errors.append(m.text) if m.type == "error" else None)
        page.on("pageerror", lambda e: console_errors.append(str(e)))

        # --- Dashboard ---
        page.goto(BASE + "/", wait_until="domcontentloaded")
        page.wait_for_timeout(2500)

        # Title / branding
        check("branding", "EasyTier Pro" in page.content())

        # Sidebar nav exists
        nav_labels = page.locator("nav button").all_inner_texts()
        check("sidebar-nav", any("Dashboard" in l for l in nav_labels) and any("Networks" in l for l in nav_labels))

        # Core status badge visible
        page.wait_for_selector(".badge", timeout=5000)
        badge = page.locator(".badge").first.inner_text()
        check("status-badge", badge.lower() in ("stopped", "starting", "running", "error"), f"status={badge}")

        # Start/Stop control present
        has_start = page.locator("button:has-text('Start Core')").count() > 0
        has_stop = page.locator("button:has-text('Stop')").count() > 0
        check("start-stop-control", has_start or has_stop, f"start={has_start} stop={has_stop}")

        # Environment card (from API)
        page.wait_for_timeout(1000)
        env = page.locator("text=Environment").count()
        check("environment-card", env > 0)

        page.screenshot(path="C:/Users/admin/AppData/Local/Temp/opencode/dashboard.png", full_page=True)

        # --- Navigate to Networks ---
        page.locator("nav button:has-text('Networks')").click()
        page.wait_for_timeout(2000)
        page.wait_for_selector("text=Manage network instance configurations", timeout=5000)
        check("networks-page", True)

        # New Network button
        new_btn = page.locator("button:has-text('New Network')")
        check("new-network-btn", new_btn.count() > 0)

        # Create a network via the modal
        new_btn.click()
        page.wait_for_selector("text=TOML", timeout=5000)
        page.locator("button:has-text('TOML')").click()
        page.wait_for_selector("textarea", timeout=5000)
        page.locator("textarea").fill(
            'instance_id = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"\n'
            'instance_name = "playwright-node"\n'
            '\n'
            '[network_identity]\n'
            'network_name = "pw-test-net"\n'
            'network_secret = ""\n'
        )
        page.locator("button:has-text('Save')").click()
        page.wait_for_timeout(2000)

        # The saved network should appear in the list
        page.wait_for_selector("text=pw-test-net", timeout=8000)
        check("network-created", True, "pw-test-net listed")

        page.screenshot(path="C:/Users/admin/AppData/Local/Temp/opencode/networks.png", full_page=True)

        # --- Peers page ---
        page.locator("nav button:has-text('Peers')").click()
        page.wait_for_timeout(2000)
        check("peers-page", page.locator("text=Connected nodes").count() > 0)

        # --- Settings page ---
        page.locator("nav button:has-text('Settings')").click()
        page.wait_for_timeout(2000)
        check("settings-page", page.locator("text=Application & environment").count() > 0)

        # Web management info
        check("web-info", page.locator("text=Web Management").count() > 0)

        page.screenshot(path="C:/Users/admin/AppData/Local/Temp/opencode/settings.png", full_page=True)

        # --- Cleanup: delete the test network ---
        page.locator("nav button:has-text('Networks')").click()
        page.wait_for_timeout(1500)
        delete_btn = page.locator("button:has-text('Delete')")
        if delete_btn.count() > 0:
            page.on("dialog", lambda d: d.accept())
            delete_btn.first.click()
            page.wait_for_timeout(1500)

        page.screenshot(path="C:/Users/admin/AppData/Local/Temp/opencode/final.png", full_page=True)

        # --- JS errors ---
        check("no-console-errors", len(console_errors) == 0, f"errors={console_errors[:5]}")

        browser.close()

    failed = [r for r in results if not r[1]]
    print(f"\n=== {len(results) - len(failed)}/{len(results)} passed ===")
    sys.exit(1 if failed else 0)

if __name__ == "__main__":
    main()


