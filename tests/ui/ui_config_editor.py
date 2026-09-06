"""Test the structured config editor dialog."""
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
        page = browser.new_page(viewport={"width": 1280, "height": 900})
        errors = []
        page.on("console", lambda m: errors.append(m.text) if m.type == "error" else None)
        page.on("pageerror", lambda e: errors.append(str(e)))

        page.goto(BASE + "/#/networks", wait_until="domcontentloaded")
        page.wait_for_timeout(2500)

        # Open New Network dialog
        page.locator("button:has-text('New Network')").click()
        page.wait_for_selector("text=New Network", timeout=5000)
        check("dialog-opens", page.locator("text=Form").count() > 0 and page.locator("text=TOML").count() > 0)

        # Fill form fields
        page.locator("input[placeholder='my-node']").fill("editor-node")
        page.locator("input[placeholder='my-network']").fill("editor-net")
        page.locator("input[placeholder*='public']").fill("secret-123")
        # disable DHCP to reveal IPv4
        dhcp_cb = page.locator("label:has-text('DHCP') input")
        if dhcp_cb.count() > 0:
            dhcp_cb.first.click()
        page.wait_for_timeout(300)
        page.locator("input[placeholder='10.144.144.1']").fill("10.99.99.1")
        check("form-filled", True)

        # Switch to TOML mode and verify generated content
        page.locator("button:has-text('TOML')").click()
        page.wait_for_timeout(400)
        toml_text = page.locator("textarea").input_value()
        check("toml-has-network", "editor-net" in toml_text, toml_text[:120])
        check("toml-has-secret", "secret-123" in toml_text)
        check("toml-has-ipv4", "10.99.99.1/24" in toml_text)

        # Save
        page.locator("button:has-text('Save')").click()
        page.wait_for_timeout(1500)
        check("config-saved", page.locator("text=editor-net").count() > 0)

        # Edit it back via form (parsing round-trip)
        page.locator("button:has-text('Edit')").first.click()
        page.wait_for_timeout(800)
        netname = page.locator("input[placeholder='my-network']").input_value()
        check("edit-loads-network", netname == "editor-net", f"netname={netname}")
        # Close the dialog
        page.locator("button:has-text('Cancel')").click()
        page.wait_for_timeout(600)

        # Cleanup
        page.on("dialog", lambda d: d.accept())
        page.locator("button:has-text('Delete')").first.click()
        page.wait_for_timeout(1200)

        check("no-errors", len(errors) == 0, f"errors={errors[:5]}")
        browser.close()

    failed = [r for r in results if not r[1]]
    print(f"\n=== {len(results) - len(failed)}/{len(results)} passed ===")
    sys.exit(1 if failed else 0)

if __name__ == "__main__":
    main()

