package syscfg

// This file contains tweaks for enabling/disabling unattended upgrades.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	errw "github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	autoUpgradesPath             = "/etc/apt/apt.conf.d/20auto-upgrades"
	autoUpgradesContentsEnabled  = `APT::Periodic::Update-Package-Lists "1";` + "\n" + `APT::Periodic::Unattended-Upgrade "1";` + "\n"
	autoUpgradesContentsDisabled = `APT::Periodic::Update-Package-Lists "1";` + "\n" + `APT::Periodic::Unattended-Upgrade "0";` + "\n"

	unattendedUpgradesPath = "/etc/apt/apt.conf.d/50unattended-upgrades"
)

type UpgradesConfig struct {
	// Type can be
	// Empty/missing ("") to make no changes
	// "disable" (or "disabled") to disable auto-upgrades
	// "security" to enable ONLY security upgrades
	// "all" to enable upgrades from all configured sources
	Type string `json:"type"`
}

func EnforceUpgrades(ctx context.Context, cfg UpgradesConfig, log *zap.SugaredLogger) {
	if cfg.Type == "" {
		return
	}

	err := checkSupportedDistro()
	if err != nil {
		log.Error(err)
		return
	}

	if cfg.Type == "disable" || cfg.Type == "disabled" {
		isNew, err := writeFileIfNew(autoUpgradesPath, []byte(autoUpgradesContentsDisabled))
		if err != nil {
			log.Error(err)
		}
		if isNew {
			log.Info("Disabled OS auto-upgrades.")
		}
		return
	}

	err = verifyInstall()
	if err != nil {
		err = doInstall(ctx)
		if err != nil {
			log.Error(err)
			return
		}
	}

	securityOnly := cfg.Type == "security"
	confContents, err := generateOrigins(securityOnly)
	if err != nil {
		log.Error(err)
		return
	}

	isNew1, err := writeFileIfNew(autoUpgradesPath, []byte(autoUpgradesContentsEnabled))
	if err != nil {
		log.Error(err)
		return
	}

	isNew2, err := writeFileIfNew(unattendedUpgradesPath, []byte(confContents))
	if err != nil {
		log.Error(err)
		return
	}

	if isNew1 || isNew2 {
		if securityOnly {
			log.Info("Enabled OS auto-upgrades (security only.)")
		} else {
			log.Info("Enabled OS auto-upgrades (full.)")
		}
	}

	err = enableTimer()
	if err != nil {
		log.Error(err)
	}
}

func checkSupportedDistro() error {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return err
	}

	if strings.Contains(string(data), "VERSION_CODENAME=bookworm") || strings.Contains(string(data), "VERSION_CODENAME=bullseye") {
		return nil
	}

	return errw.New("cannot enable automatic upgrades for unknown distro, only support for Debian bullseye and bookworm is available")
}

// make sure the needed package is installed.
func verifyInstall() error {
	cmd := exec.Command("unattended-upgrade", "-h")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errw.Wrapf(err, "executing 'unattended-upgrade -h' %s", output)
	}
	return nil
}

func enableTimer() error {
	// enable here
	cmd := exec.Command("systemctl", "enable", "apt-daily-upgrade.timer")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errw.Wrapf(err, "executing 'systemctl enable apt-daily-upgrade.timer' %s", output)
	}
	return nil
}

func doInstall(ctx context.Context) error {
	// On low bandwidth connections, apt updates/installs can take a while, so start something to handle healthchecks
	sleepCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		HealthySleep(sleepCtx, time.Hour)
	}()

	cmd := exec.CommandContext(ctx, "apt", "update")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errw.Wrapf(err, "executing 'apt update' %s", output)
	}

	cmd = exec.CommandContext(ctx, "apt", "install", "-y", "unattended-upgrades")
	output, err = cmd.CombinedOutput()
	if err != nil {
		return errw.Wrapf(err, "executing 'apt install -y unattended-upgrades' %s", output)
	}

	return nil
}

// generates the "Origins-Pattern" section of 50unattended-upgrades file.
func generateOrigins(securityOnly bool) (string, error) {
	cmd := exec.Command("apt-cache", "policy")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", errw.Wrapf(err, "executing 'apt-cache policy' %s", output)
	}

	releaseRegex := regexp.MustCompile(`release.*o=([^,]+).*n=([^,]+).*`)
	matches := releaseRegex.FindAllStringSubmatch(string(output), -1)

	// use map to reduce to unique set
	releases := map[string]bool{}
	for _, release := range matches {
		// we expect at least an origin and a codename from each line
		if len(release) != 3 {
			continue
		}
		if securityOnly && !strings.Contains(release[2], "security") {
			continue
		}
		releases[fmt.Sprintf(`"origin=%s,codename=%s";`, release[1], release[2])] = true
	}

	// generate actual file contents
	origins := "Unattended-Upgrade::Origins-Pattern {"
	for release := range releases {
		origins = fmt.Sprintf("%s\n    %s", origins, release)
	}
	origins = fmt.Sprintf("%s\n};\n", origins)
	return origins, nil
}
