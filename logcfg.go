package syscfg

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"regexp"

	errw "github.com/pkg/errors"
	sysd "github.com/sergeymakinen/go-systemdconf/v2"
	"go.uber.org/zap"

	"github.com/sergeymakinen/go-systemdconf/v2/conf"
)

var (
	journaldConfPath = "/etc/systemd/journald.conf.d/90-viam.conf"
	defaultLogLimit = "512M"
)

type LogConfig struct {
	SystemMaxUse string `json:"system_max_use"`
	RuntimeMaxUse  string `json:"runtime_max_use"`
}

func EnforceLogging(cfg LogConfig, log *zap.SugaredLogger) {
	cmd := exec.Command("systemctl", "is-enabled", "systemd-journald")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error(errw.Wrapf(err, "executing 'systemctl is-enabled systemd-journald' %s", output))
		log.Error("agent-syscfg can only adjust logging settings for systems using systemd with journald enabled")
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
	if ! (sizeRegEx.MatchString(persistSize) && sizeRegEx.MatchString(tempSize)) {
		log.Error(errw.New("logfile size limits must be specificed in bytes, with one optional suffix character [KMGTPE]"))
		return
	}

	journalConf := &conf.JournaldFile{
		Journal: conf.JournaldJournalSection{
			SystemMaxUse: sysd.Value{persistSize},
			RuntimeMaxUse: sysd.Value{tempSize},
		},
	}

	newFileBytes, err := sysd.Marshal(journalConf)
	if err != nil {
		log.Error(errw.Wrapf(err, "could not marshal new file for %s", journaldConfPath))
		return
	}

	//nolint:gosec
	curFileBytes, err := os.ReadFile(journaldConfPath)
	if err != nil {
		if !errw.Is(err, fs.ErrNotExist) {
			log.Error(errw.Wrapf(err, "opening %s for reading", journaldConfPath))
			return
		}
	} else if bytes.Equal(curFileBytes, newFileBytes) {
		return
	}

	log.Infof("Updating %s, setting SystemMaxUse=%s and RuntimeMaxUse=%s", journaldConfPath, persistSize, tempSize)

	if err := os.MkdirAll(path.Dir(journaldConfPath), 0o755); err != nil {
		log.Error(errw.Wrapf(err, "creating directory for %s", journaldConfPath))
	}

	//nolint:gosec
	if err := os.WriteFile(journaldConfPath, newFileBytes, 0o644); err != nil {
		log.Error(errw.Wrapf(err, "writing %s", journaldConfPath))
		return
	}

	cmd = exec.Command("systemctl", "restart", "systemd-journald")
	output, err = cmd.CombinedOutput()
	if err != nil {
		log.Error(errw.Wrapf(err, "executing 'systemctl restart systemd-journald' %s", output))
		return
	}
}
