"""Test language switching and theme switching."""
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

        # Default: English
        check("english-default", page.locator("text=Dashboard").count() > 0)

        # Switch to Chinese via sidebar toggle
        page.locator("nav button:has-text('中文')").click()
        page.wait_for_timeout(800)
        check("zh-switched", page.locator("text=仪表盘").count() > 0, "")
        page.screenshot(path="C:/Users/admin/AppData/Local/Temp/opencode/zh.png", full_page=True)

        # Navigate to Networks - should be Chinese
        page.locator("nav button:has-text('网络')").click()
        page.wait_for_timeout(1500)
        check("zh-networks", page.locator("text=管理网络实例配置").count() > 0)

        # Switch back to English
        page.locator("nav button:has-text('English')").click()
        page.wait_for_timeout(800)
        check("en-switched", page.locator("text=Networks").count() > 0)

        # Theme toggle
        toggle = page.locator("nav button:has-text('日间')")
        if toggle.count() > 0:
            toggle.click()
            page.wait_for_timeout(800)
            html_class = page.evaluate("document.documentElement.className")
            check("light-theme", "dark" not in html_class, f"class={html_class}")
        else:
            toggle = page.locator("nav button:has-text('夜间')")
            if toggle.count() > 0:
                toggle.click()
                page.wait_for_timeout(800)
                html_class = page.evaluate("document.documentElement.className")
                check("light-theme", "dark" in html_class, f"class={html_class}")
            else:
                check("theme-toggle", False, "no toggle button")

        page.screenshot(path="C:/Users/admin/AppData/Local/Temp/opencode/light.png", full_page=True)

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
