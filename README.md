# Agent Syscfg
This is a subsystem (plugin) for the viam-agent that provides a number of system/os configuration helpers.

## Current Options
Parameters are set via the `attributes` object of the agent-syscfg object in the agent config (currently via "Raw JSON" editor in https://app.viam.com/ )

Configuration is split into sections, each of which will control a different area of system management and/or configuration. Currently only logging and automatic upgrade control is available.

### Logging
Two parameters can be set for logging control. `system_max_use` and `runtime_max_use` The first sets the maximum disk space journald will user for persistent log storage. The second, the runtime/temporary limit. Both of these will be configured to 512M by default if not set. Numeric values are in bytes, with optional single letter suffix for larger units, e.g. K, M, or G.
There is also `disable` which may be set to `true` to remove any prior tweaks to the logging config and disable the use of defaults.

### Automatic Upgrades
This enables (or disables) the "unattended upgrades" functionality in Debian (currently only bullseye or bookworm.) Set the `type` parameter to one of the following:
* `` (blank or unset, default) will do/change nothing
* `disable` will actively disable automatic upgrades
* `security` will only enable updates from sources with `security` in their codename, ex: `bookworm-security`
* `all` will enable updates from all configured repos/sources

Note that this will install the `unattended-upgrades` package, and then replace `20auto-upgrades` and `50unattended-upgrades` in `/etc/apt/apt.conf.d/`, with the latter's Origins-Pattern list being generated automatically from configured repositories on the system, so custom repos (at the time the setting is enabled) will be included.

## Example Config

```json
"agent-syscfg": {
	"release_channel": "stable",
	"attributes": {
		"logging": {
			"disable": true,
			"system_max_use": "128M",
			"runtime_max_use": "96M"
		},
		"upgrades": {
			"type": "all"
		}
	}
}
```
