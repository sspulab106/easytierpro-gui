"""Test advanced config editor features + full flag coverage."""
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
        page = browser.new_page(viewport={"width": 1400, "height": 1000})
        errors = []
        page.on("console", lambda m: errors.append(m.text) if m.type == "error" else None)
        page.on("pageerror", lambda e: errors.append(str(e)))

        page.goto(BASE + "/#/networks", wait_until="domcontentloaded")
        page.wait_for_timeout(2500)

        page.locator("button:has-text('New Network')").click()
        page.wait_for_selector("text=Basic", timeout=5000)

        page.locator("input[placeholder='my-node']").fill("adv-node")
        page.locator("input[placeholder='my-network']").fill("adv-net")
        page.locator("input[placeholder*='public']").fill("secret")

        page.locator("label:has-text('DHCP') input").first.click()
        page.wait_for_timeout(300)
        page.locator("input[placeholder='10.144.144.1']").fill("10.50.50.1")

        page.locator("input[placeholder*='peer.example.com']").fill("wss://ez.cloud.c01.kr")
        page.locator("button:has-text('Peer')").last.click()

        page.locator("textarea[placeholder='10.100.1.0/24']").fill("10.100.1.0/24\n192.168.1.0/24")
        page.locator("input[placeholder='1080']").fill("1080")

        advanced = {
            "延迟优先": True,
            "用户态协议栈": True,
            "禁用 IPv6": True,
            "启用 KCP 代理": True,
            "禁用 QUIC 输入": True,
            "仅使用物理网卡": True,
            "启用多线程": True,
            "禁用加密": True,
            "禁用 TCP 打洞": True,
            "启用魔法 DNS": True,
        }
        for label, want in advanced.items():
            cb = page.locator(f"label:has-text('{label}') input").first
            if cb.count() > 0:
                is_checked = cb.is_checked()
                if want != is_checked:
                    cb.click()
            else:
                check(f"flag-{label}-exists", False, "checkbox not found")
        check("flags-toggled", True)

        page.locator("input[placeholder='0.0.0.0:11015']").fill("0.0.0.0:11015")
        page.locator("button:has-text('Add client')").click()
        page.wait_for_timeout(300)
        page.locator("input[placeholder*='client name']").fill("phone")
        page.locator("input[placeholder='10.144.144.3']").fill("10.144.144.3")

        page.locator("button:has-text('TOML')").click()
        page.wait_for_timeout(400)
        toml = page.locator("textarea").input_value()

        check("toml-relay-peer", "wss://ez.cloud.c01.kr" in toml, toml[:200])
        check("toml-proxy", "10.100.1.0/24" in toml)
        check("toml-socks5", "socks5://0.0.0.0:1080" in toml)
        check("toml-latency", "latency_first = true" in toml)
        check("toml-smoltcp", "use_smoltcp = true" in toml)
        check("toml-ipv6", "enable_ipv6 = false" in toml)
        check("toml-kcp", "enable_kcp_proxy = true" in toml)
        check("toml-quic-input", "disable_quic_input = true" in toml)
        check("toml-bind-device", "bind_device = true" in toml)
        check("toml-multithread", "multi_thread = true" in toml)
        check("toml-no-encrypt", "enable_encryption = false" in toml)
        check("toml-tcp-hole", "disable_tcp_hole_punching = true" in toml)
        check("toml-magic-dns", "accept_dns = true" in toml)
        check("toml-vpn-portal", 'wireguard_listen = "0.0.0.0:11015"' in toml)
        check("toml-vpn-client", 'name = "phone"' in toml and "10.144.144.3" in toml)

        page.locator("button:has-text('Save')").click()
        page.wait_for_timeout(1500)
        check("config-saved", page.locator("text=adv-net").count() > 0)

        page.locator("button:has-text('Edit')").first.click()
        page.wait_for_timeout(800)
        page.locator("button:has-text('Form')").click()
        page.wait_for_timeout(400)
        latency_checked = page.locator("label:has-text('延迟优先') input").first.is_checked()
        check("roundtrip-latency", latency_checked, f"latency_first={latency_checked}")
        socks = page.locator("input[placeholder='1080']").input_value()
        check("roundtrip-socks5", socks == "1080", f"socks5={socks}")
        ipv4 = page.locator("input[placeholder='10.144.144.1']").input_value()
        check("roundtrip-ipv4", ipv4 == "10.50.50.1", f"ipv4={ipv4}")

        page.locator("button:has-text('Cancel')").click()
        page.wait_for_timeout(500)
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
