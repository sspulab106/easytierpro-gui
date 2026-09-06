"""Verify multi-network free listener port auto-assignment."""
import sys
import os
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__))))
from common import base_url
from playwright.sync_api import sync_playwright

BASE = base_url()


def clear_configs(page):
    return page.evaluate("""async () => {
        const cfg = await fetch('/webconfig.json').then(r => r.json());
        const res = await fetch('/api/configs', { headers: { 'X-Auth-Token': cfg.token } });
        const data = await res.json();
        for (const c of data) {
            await fetch('/api/config?id=' + encodeURIComponent(c.instance_id),
                { method: 'DELETE', headers: { 'X-Auth-Token': cfg.token } });
        }
        return data.length;
    }""")


def main():
    results = []
    def check(name, ok, detail=""):
        results.append((name, ok, detail))
        print(("PASS" if ok else "FAIL"), name, detail)

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page(viewport=dict(width=1280, height=900))
        errs = []
        page.on('console', lambda m: errs.append(m.text) if m.type == 'error' else None)

        page.goto(BASE + '/#/networks', wait_until='domcontentloaded')
        page.wait_for_timeout(2000)
        cleared = clear_configs(page)
        page.wait_for_timeout(1500)
        page.reload()
        page.wait_for_timeout(2000)

        # First network (empty config list -> port 11010)
        page.locator('button:has-text("New Network")').click()
        page.wait_for_selector('text=Basic', timeout=5000)
        page.locator('input[placeholder="my-node"]').fill('net-one')
        page.locator('input[placeholder="my-network"]').fill('net-one')
        page.locator('button:has-text("TOML")').click()
        page.wait_for_timeout(400)
        toml1 = page.locator('textarea').input_value()
        check('net-one-port-11010', ':11010' in toml1, toml1[:180])
        page.locator('button:has-text("Save")').click()
        page.wait_for_timeout(2500)
        check('net-one-created', page.locator('text=net-one').count() > 0)

        # Second network -> should get port 11011 (free)
        page.locator('button:has-text("New Network")').click()
        page.wait_for_selector('text=Basic', timeout=5000)
        page.locator('input[placeholder="my-node"]').fill('net-two')
        page.locator('input[placeholder="my-network"]').fill('net-two')
        page.locator('button:has-text("TOML")').click()
        page.wait_for_timeout(400)
        toml2 = page.locator('textarea').input_value()
        check('net-two-port-free', ':11011' in toml2, toml2[:180])
        page.locator('button:has-text("Save")').click()
        page.wait_for_timeout(2500)
        check('net-two-created', page.locator('text=net-two').count() > 0)

        # Third network -> port 11012
        page.locator('button:has-text("New Network")').click()
        page.wait_for_selector('text=Basic', timeout=5000)
        page.locator('input[placeholder="my-node"]').fill('net-three')
        page.locator('input[placeholder="my-network"]').fill('net-three')
        page.locator('button:has-text("TOML")').click()
        page.wait_for_timeout(400)
        toml3 = page.locator('textarea').input_value()
        check('net-three-port-free', ':11012' in toml3, toml3[:180])
        page.locator('button:has-text("Save")').click()
        page.wait_for_timeout(2500)
        check('net-three-created', page.locator('text=net-three').count() > 0)

        # No conflict warning when ports are distinct
        page.locator('button:has-text("Edit")').nth(1).click()
        page.wait_for_timeout(800)
        warn = page.locator('text=Listener port already in use').count()
        check('no-conflict-warning', warn == 0, f"warning={warn}")
        page.locator('button:has-text("Cancel")').click()
        page.wait_for_timeout(500)

        # Conflict warning when manually forcing an in-use port
        page.locator('button:has-text("Edit")').nth(1).click()
        page.wait_for_selector('text=Basic', timeout=5000)
        first_listener = page.locator('input.font-mono').first
        first_listener.fill('tcp://0.0.0.0:11010')
        page.wait_for_selector('text=Listener port already in use', timeout=5000)
        warn2 = page.locator('text=Listener port already in use').count()
        check('conflict-warning-shown', warn2 > 0, f"warning={warn2}")
        page.locator('button:has-text("Cancel")').click()
        page.wait_for_timeout(500)

        # Cleanup: delete all configs
        page.on('dialog', lambda d: d.accept())
        for _ in range(4):
            del_btn = page.locator('button:has-text("Delete")')
            if del_btn.count() == 0:
                break
            del_btn.first.click()
            page.wait_for_timeout(1500)
        remaining = page.locator('text=net-one,text=net-two,text=net-three').count()
        check('cleanup', remaining == 0, f"remaining={remaining}")

        check('no-errors', len(errs) == 0, f"errors={errs[:3]}")
        browser.close()

    failed = [r for r in results if not r[1]]
    print(f"\n=== {len(results) - len(failed)}/{len(results)} passed ===")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
