"""Test: create network via Networks page, verify it appears in Dashboard with enable toggle."""
import sys
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
        page = browser.new_page(viewport={"width": 1400, "height": 900})
        page.goto(BASE + "/", wait_until="networkidle", timeout=30000)
        page.wait_for_selector("main", timeout=20000)

        # Go to Networks page, create a network
        page.locator("nav button:has-text('Networks')").click()
        page.wait_for_timeout(1500)
        page.locator("button:has-text('New Network')").click()
        page.wait_for_selector("text=Basic", timeout=5000)

        # TOML mode: relay-only config with valid UUID + no_tun
        page.locator("button:has-text('TOML')").click()
        page.wait_for_selector("textarea", timeout=5000)
        page.locator("textarea").fill(
            'instance_id = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"\n'
            'instance_name = "dash-test"\n'
            'peers = ["wss://ez.cloud.c01.kr"]\n'
            '\n'
            '[network_identity]\n'
            'network_name = "dash-net"\n'
            'network_secret = ""\n'
            '\n'
            '[flags]\n'
            'no_tun = true\n'
        )
        page.locator("button:has-text('Save')").click()
        page.wait_for_timeout(3000)

        # Verify network created
        page.wait_for_selector("text=dash-net", timeout=10000)
        check("net-created", page.locator("text=dash-net").count() > 0)

        # Dashboard should show the network (enabled)
        page.goto(BASE + "/", wait_until="networkidle", timeout=30000)
        page.wait_for_timeout(2500)
        dash = page.locator("body").inner_text()
        check("dash-shows-network", "dash-net" in dash, "")
        check("dash-shows-enabled", "enabled" in dash.lower() or "已启用" in dash, "")

        # Core should be running and loading the config
        page.wait_for_selector(".badge", timeout=10000)
        core_running = page.locator(".badge").first.inner_text()
        check("core-running", "running" in core_running.lower(), f"badge={core_running}")

        # Peers page shows the table header
        page.locator("nav button:has-text('Peers')").click()
        page.wait_for_timeout(3000)
        peers = page.locator("body").inner_text()
        check("peers-shows-rows", "Hostname" in peers or "主机名" in peers, "")

        # Disable the network via Dashboard
        page.locator("nav button:has-text('Dashboard')").click()
        page.wait_for_timeout(2000)
        for _ in range(10):
            if "dash-net" in page.locator("body").inner_text():
                break
            page.wait_for_timeout(1000)
        disable_btn = page.locator("button:has-text('Disable'),button:has-text('禁用')")
        if disable_btn.count() > 0:
            disable_btn.first.click()
            page.wait_for_timeout(4000)
            dash3 = page.locator("body").inner_text()
            check("dash-network-disabled", "disabled" in dash3.lower() or "已禁用" in dash3, "")
        else:
            check("dash-network-disabled", False, "no disable button")

        # Cleanup: delete the config
        page.locator("nav button:has-text('Networks')").click()
        page.wait_for_timeout(1500)
        page.on("dialog", lambda d: d.accept())
        del_btn = page.locator("button:has-text('Delete')")
        if del_btn.count() > 0:
            del_btn.first.click()
            page.wait_for_timeout(2000)
            check("net-deleted", page.locator("text=dash-net").count() == 0, "")
        else:
            check("net-deleted", False, "no delete button")

        errors = []
        page.on("pageerror", lambda e: errors.append(str(e)))
        page.wait_for_timeout(1000)
        check("no-errors", len(errors) == 0, f"errors={errors[:3]}")

        browser.close()

    failed = [r for r in results if not r[1]]
    print(f"\n=== {len(results) - len(failed)}/{len(results)} passed ===")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
