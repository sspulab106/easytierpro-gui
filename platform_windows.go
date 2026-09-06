//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runKeyValue  = "EasyTier Pro"
	toastAppIDPS = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`
)

// getAutoStart reports whether the autostart scheduled task exists (the
// legacy HKCU Run entry also counts, so pre-migration installs keep the
// checkbox state).
func getAutoStart() bool {
	if taskExists() {
		return true
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runKeyValue)
	return err == nil && v != ""
}

// setAutoStart toggles launch-at-login. A scheduled task with RUNLEVEL_HIGHEST
// is used instead of the HKCU Run key: the GUI embeds a requireAdministrator
// manifest, and Windows silently drops Run-key launches of elevated apps —
// the task scheduler starts the same binary elevated without an extra UAC
// prompt. Falls back to the Run key when task creation fails (e.g. no task
// scheduler service), which still works for non-elevated installs.
func setAutoStart(enable bool) error {
	if !enable {
		deleteTask()
		// Also clear a legacy Run entry left by older versions.
		k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err == nil {
			_ = k.DeleteValue(runKeyValue)
			k.Close()
		}
		return nil
	}
	if err := createTask(); err == nil {
		return nil
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(runKeyValue, fmt.Sprintf(`"%s" --minimized`, exe))
}

// taskName is the scheduled task backing the autostart toggle.
const taskName = `EasyTierProAutostart`

// taskExists reports whether the autostart task is registered.
func taskExists() bool {
	cmd := exec.Command("schtasks", "/Query", "/TN", taskName)
	hideConsole(cmd)
	return cmd.Run() == nil
}

// createTask registers a per-user logon task: runs the GUI minimized with an
// elevated token (no extra UAC prompt at logon), unrestricted time limit.
// Registering a RunLevel=Highest task needs an elevated caller — the GUI runs
// elevated by manifest, so this succeeds from the settings toggle.
func createTask() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	script := fmt.Sprintf(`
$action = New-ScheduledTaskAction -Execute %s -Argument '--minimized'
$user = "$env:USERDOMAIN\$env:USERNAME"
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $user
$principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit ([TimeSpan]::Zero)
Register-ScheduledTask -TaskName '%s' -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force -ErrorAction Stop | Out-Null
`, psSingleQuote(exe), taskName)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	hideConsole(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("task registration failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// deleteTask unregisters the autostart task (ignored when absent).
func deleteTask() {
	script := fmt.Sprintf(`Unregister-ScheduledTask -TaskName '%s' -Confirm:$false -ErrorAction SilentlyContinue`, taskName)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	hideConsole(cmd)
	_ = cmd.Run()
}

// copyToClipboard places text on the Windows clipboard via PowerShell.
func copyToClipboard(text string) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Set-Clipboard -Value "+psSingleQuote(text))
	hideConsole(cmd)
	_ = cmd.Run()
}

// psSingleQuote quotes a string for safe inclusion in a PowerShell command.
func psSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// desktopNotify shows a Windows toast via an inline PowerShell script (no
// third-party dependencies). The notifier AppID borrows PowerShell's own
// registered AUMID so the toast is allowed without an installed app identity.
func desktopNotify(title, body string) error {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := fmt.Sprintf(`
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$null = $xml.GetElementsByTagName('text').Item(0).AppendChild($xml.CreateTextNode(%s))
$null = $xml.GetElementsByTagName('text').Item(1).AppendChild($xml.CreateTextNode(%s))
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('%s').Show($toast)
`, q(title), q(body), toastAppIDPS)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	hideConsole(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("toast notify failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
