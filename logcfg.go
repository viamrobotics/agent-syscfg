package syscfg

// This file contains tweaks for logging/journald, such as max size limits.

import (
	"io/fs"
	"os"
	"os/exec"
	"regexp"

	errw "github.com/pkg/errors"
	sysd "github.com/sergeymakinen/go-systemdconf/v2"
	"github.com/sergeymakinen/go-systemdconf/v2/conf"
	"go.uber.org/zap"
)

var (
	journaldConfPath = "/etc/systemd/journald.conf.d/90-viam.conf"
	defaultLogLimit  = "512M"
)

type LogConfig struct {
	Disable       bool   `json:"disable"`
	SystemMaxUse  string `json:"system_max_use"`
	RuntimeMaxUse string `json:"runtime_max_use"`
}

func EnforceLogging(cfg LogConfig, log *zap.SugaredLogger) {
	if cfg.Disable {
		if err := os.Remove(journaldConfPath); err != nil {
			if errw.Is(err, fs.ErrNotExist) {
				return
			}
			log.Error(errw.Wrapf(err, "deleting %s", journaldConfPath))
			return
		}

		if !checkJournaldEnabled(log) {
			return
		}

		if err := restartJournald(); err != nil {
			log.Error(err)
			return
		}
		log.Infof("Logging config disabled. Removing customized %s", journaldConfPath)
		return
	}

	if !checkJournaldEnabled(log) {
		return
	}

	persistSize := cfg.SystemMaxUse
	tempSize := cfg.RuntimeMaxUse

	if persistSize == "" {
		persistSize = defaultLogLimit
	}

	if tempSize == "" {
		tempSize = defaultLogLimit
	}

	sizeRegEx := regexp.MustCompile(`^[0-9]+[KMGTPE]$`)
	if !(sizeRegEx.MatchString(persistSize) && sizeRegEx.MatchString(tempSize)) {
		log.Error(errw.New("logfile size limits must be specificed in bytes, with one optional suffix character [KMGTPE]"))
		return
	}

	journalConf := &conf.JournaldFile{
		Journal: conf.JournaldJournalSection{
			SystemMaxUse:  sysd.Value{persistSize},
			RuntimeMaxUse: sysd.Value{tempSize},
		},
	}

	newFileBytes, err := sysd.Marshal(journalConf)
	if err != nil {
		log.Error(errw.Wrapf(err, "marshaling new file for %s", journaldConfPath))
		return
	}

	isNew, err := writeFileIfNew(journaldConfPath, newFileBytes)
	if err != nil {
		log.Error(err)
		// We may have written a corrupt file, try to remove to salvage at least default behavior.
		if err := os.RemoveAll(journaldConfPath); err != nil {
			log.Error(errw.Wrapf(err, "deleting %s", journaldConfPath))
		}
		return
	}

	if isNew {
		if err := restartJournald(); err != nil {
			log.Error(err)
			return
		}
		log.Infof("Updated %s, setting SystemMaxUse=%s and RuntimeMaxUse=%s", journaldConfPath, persistSize, tempSize)
	}
}

func restartJournald() error {
	cmd := exec.Command("systemctl", "restart", "systemd-journald")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errw.Wrapf(err, "executing 'systemctl restart systemd-journald' %s", output)
	}
	return nil
}

func checkJournaldEnabled(log *zap.SugaredLogger) bool {
	cmd := exec.Command("systemctl", "is-enabled", "systemd-journald")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error(errw.Wrapf(err, "executing 'systemctl is-enabled systemd-journald' %s", output))
		log.Error("agent-syscfg can only adjust logging settings for systems using systemd with journald enabled")
		return false
	}
	return true
}
