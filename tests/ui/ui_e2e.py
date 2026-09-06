"""End-to-end test: start core via UI, verify real network status appears."""
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
        page = browser.new_page(viewport={"width": 1280, "height": 800})
        errors = []
        page.on("console", lambda m: errors.append(m.text) if m.type == "error" else None)
        page.on("pageerror", lambda e: errors.append(str(e)))

        page.goto(BASE + "/", wait_until="domcontentloaded")
        page.wait_for_timeout(2000)

        # Ensure a test network config exists
        page.locator("nav button:has-text('Networks')").click()
        page.wait_for_timeout(1500)
        if page.locator("button:has-text('New Network')").count() == 0:
            page.wait_for_timeout(1000)
        page.locator("button:has-text('New Network')").click()
        page.wait_for_selector("text=TOML", timeout=5000)
        page.locator("button:has-text('TOML')").click()
        page.wait_for_selector("textarea", timeout=5000)
        page.locator("textarea").fill(
            'instance_id = "99999999-0000-0000-0000-000000000001"\n'
            'instance_name = "e2e-node"\n'
            'listeners = ["tcp://0.0.0.0:11010", "udp://0.0.0.0:11010"]\n'
            '\n'
            '[network_identity]\n'
            'network_name = "e2e-test-net"\n'
            'network_secret = ""\n'
        )
        page.locator("button:has-text('Save')").click()
        page.wait_for_timeout(1500)
        check("e2e-config-created", page.locator("text=e2e-test-net").count() > 0)

        # Apply & restart to load config into core
        page.locator("button:has-text('Apply & Restart')").click()
        page.wait_for_timeout(4000)

        # Navigate to Dashboard; core may already be running from Apply & Restart.
        page.locator("nav button:has-text('Dashboard')").click()
        page.wait_for_timeout(1500)
        start_btn = page.locator("button:has-text('Start Core')")
        stop_btn = page.locator("button:has-text('Stop')")
        if start_btn.count() > 0:
            start_btn.click()
            check("core-start-clicked", True)
        elif stop_btn.count() > 0:
            check("core-start-clicked", True, "already running")
        else:
            check("core-start-clicked", False, "no start/stop button")

        # Wait for core to become running
        running = False
        for _ in range(30):
            page.wait_for_timeout(1000)
            badge = page.locator(".badge").first.inner_text()
            if badge.lower() == "running":
                running = True
                break
        check("core-becomes-running", running, f"last badge={badge}")

        # Dashboard should show node info (Peer ID, Hostname, Version)
        # Allow retries: core's RPC portal takes a moment to become ready.
        stats_ok = False
        version_ok = False
        for _ in range(15):
            page.wait_for_timeout(1500)
            stats_text = page.locator("main").inner_text()
            # stat labels are rendered uppercase via CSS text-transform
            if "PEER ID" in stats_text and "HOSTNAME" in stats_text:
                stats_ok = True
            if "2.6.4" in stats_text:
                version_ok = True
            if stats_ok and version_ok:
                break
        check("stats-shown", stats_ok, "")
        check("version-shown", version_ok, "")

        # Diagnostic: call /api/node from within the page (uses the loaded token)
        node_result = page.evaluate("""async () => {
            const cfg = await fetch('/webconfig.json').then(r => r.json());
            const res = await fetch('/api/node', { headers: { 'X-Auth-Token': cfg.token } });
            if (res.ok) {
                const d = await res.json();
                return { ok: true, peer_id: d.peer_id, hostname: d.hostname };
            }
            const text = await res.text();
            return { ok: false, status: res.status, body: text.slice(0, 200) };
        }""")
        check("api-node-direct", node_result.get("ok", False), str(node_result))

        page.screenshot(path="C:/Users/admin/AppData/Local/Temp/opencode/e2e-dashboard-running.png", full_page=True)

        # Peers page should list the local node (hostname = machine name)
        page.locator("nav button:has-text('Peers')").click()
        page.wait_for_timeout(3500)
        peers_text = page.locator("main").inner_text()
        check("peers-has-rows", ("Local" in peers_text or "Hostname" in peers_text), "")
        check("peers-shows-node", len(page.locator("tbody tr").all()) > 0, f"rows={len(page.locator('tbody tr').all())}")
        page.screenshot(path="C:/Users/admin/AppData/Local/Temp/opencode/e2e-peers.png", full_page=True)

        # Stop core via UI
        page.locator("nav button:has-text('Dashboard')").click()
        page.wait_for_timeout(1200)
        stop_btn = page.locator("button:has-text('Stop')")
        if stop_btn.count() > 0:
            stop_btn.click()
            stopped = False
            for _ in range(20):
                page.wait_for_timeout(1000)
                badge = page.locator(".badge").first.inner_text()
                if badge.lower() in ("stopped", "error"):
                    stopped = True
                    break
            check("core-stopped", stopped, f"last badge={badge}")
        else:
            check("core-stopped", False, "no stop button")

        check("no-errors", len(errors) == 0, f"errors={errors[:5]}")

        browser.close()

    failed = [r for r in results if not r[1]]
    print(f"\n=== {len(results) - len(failed)}/{len(results)} passed ===")
    sys.exit(1 if failed else 0)

if __name__ == "__main__":
    main()

