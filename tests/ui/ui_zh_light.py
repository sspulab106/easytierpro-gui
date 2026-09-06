"""Verify Chinese rendering and light theme contrast."""
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

        # Switch to Chinese
        page.locator("nav button:has-text('中文')").click()
        page.wait_for_timeout(800)

        # Check Chinese renders (not mojibake) in several places
        texts = page.locator("body").inner_text()
        check("zh-dashboard", "仪表盘" in texts, "")
        check("zh-title", "EasyTier" in texts, "")

        # Open Networks page, check Chinese
        page.locator("nav button:has-text('网络')").click()
        page.wait_for_timeout(1500)
        nets = page.locator("main").inner_text()
        check("zh-networks", "管理网络实例配置" in nets, "")
        check("zh-newbtn", "新建网络" in nets, "")

        # Open new network dialog, check the advanced flags (Chinese, hardcoded)
        page.locator("button:has-text('新建网络')").click()
        page.wait_for_selector("text=高级", timeout=5000)
        dlg = page.locator(".card").last.inner_text()
        check("zh-advanced-flags", "延迟优先" in dlg and "用户态协议栈" in dlg, "")

        # Switch to Chinese in the dialog check of VPN section
        check("zh-vpn", "VPN 门户" in dlg, "")

        # Close dialog
        page.locator("button:has-text('取消')").click()
        page.wait_for_timeout(500)

        # Switch to light theme
        toggle = page.locator("nav button:has-text('夜间')")
        if toggle.count() > 0:
            toggle.click()  # from dark, '夜间' means click to go... check label
            page.wait_for_timeout(800)
        # The sidebar shows '☀ 日间' when dark (click to go light). Let's force light.
        page.evaluate("localStorage.setItem('et-theme','light'); location.reload()")
        page.wait_for_timeout(3000)
        page.wait_for_selector("main", timeout=20000)

        # Verify html has no dark class
        cls = page.evaluate("document.documentElement.className")
        check("light-html", "dark" not in cls, f"class={cls}")

        # Verify computed background + text colors have contrast
        page.locator("nav button:has-text('网络')").click()
        page.wait_for_timeout(1500)
        contrast = page.evaluate("""() => {
            const main = document.querySelector('main');
            const h1 = main.querySelector('h1');
            const bodyBg = getComputedStyle(document.body).backgroundColor;
            const h1Color = getComputedStyle(h1).color;
            return { bodyBg, h1Color };
        }""")
        print("LIGHT COLORS:", contrast)
        # bodyBg should be light, h1 should be dark
        is_light_bg = contrast["bodyBg"].startswith("rgb(") and int(contrast["bodyBg"].split("(")[1].split(",")[0]) > 200
        is_dark_text = contrast["h1Color"].startswith("rgb(") and int(contrast["h1Color"].split("(")[1].split(",")[0]) < 60
        check("light-contrast", is_light_bg and is_dark_text, f"bg={contrast['bodyBg']} text={contrast['h1Color']}")

        page.screenshot(path="C:/Users/admin/AppData/Local/Temp/opencode/light_zh.png", full_page=True)

        # Back to dark for other tests
        page.evaluate("localStorage.setItem('et-theme','dark'); location.reload()")
        page.wait_for_timeout(3000)

        browser.close()

    failed = [r for r in results if not r[1]]
    print(f"\n=== {len(results) - len(failed)}/{len(results)} passed ===")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
