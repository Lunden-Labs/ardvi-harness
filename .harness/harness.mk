ARDVI_HARNESS_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
ARDVI_HARNESS_SCRIPTS_DIR := $(ARDVI_HARNESS_DIR)/scripts
ARDVI_HARNESS_PROJECT_ROOT := $(abspath $(ARDVI_HARNESS_DIR)/..)

.PHONY: harness-help harness-copy harness-init harness-update harness-up harness-down harness-status harness-architect harness-improve harness-bootstrap harness-register harness-doctor

harness-help:
	@echo "ARDVI harness"
	@echo "  make harness-copy [TARGET=/path]  Copy harness into a Git root"
	@echo "  make harness-init       First-time project and CAO initialization"
	@echo "  make harness-update     Update CAO, Addy skills, Agency Agents and Ponytail"
	@echo "  make harness-up         Start the local CAO control plane"
	@echo "  make harness-down       Stop all CAO sessions and the control plane"
	@echo "  make harness-status     Show local CAO status"
	@echo "  make harness-architect  Open the project architect in this terminal"
	@echo "  make harness-improve    Ask Codex for one focused harness improvement"
	@echo "  make harness-doctor     Validate harness dependencies and registration"

harness-copy:
	@TARGET="$(TARGET)" bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/copy.sh"

harness-init:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/bootstrap.sh"
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/install.sh"
	@python3 "$(ARDVI_HARNESS_SCRIPTS_DIR)/register_cao.py"
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/doctor.sh"

harness-update:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/install.sh"
	@python3 "$(ARDVI_HARNESS_SCRIPTS_DIR)/register_cao.py"

harness-up:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/server.sh"

harness-down:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/down.sh"

harness-status:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/status.sh"

harness-architect:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/launch.sh"

harness-improve:
	@codex -C "$(ARDVI_HARNESS_PROJECT_ROOT)" 'Read AGENTS.md and .harness/README.md first. Analyze the harness before editing. Propose and implement only one small, reviewable portability or safety improvement. Edit only .harness/** and necessary root harness or bootstrap documentation or Makefile entries. Do not edit product code, product documentation or configuration, dependencies, or generated files. Do not add secrets, absolute local paths, global-path coupling, or unrelated refactors. Do not commit or push. Run the narrowest relevant checks.'

harness-bootstrap:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/bootstrap.sh"

harness-register:
	@python3 "$(ARDVI_HARNESS_SCRIPTS_DIR)/register_cao.py"

harness-doctor:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/doctor.sh"

ifeq ($(ARDVI_HARNESS_SHORT_TARGETS),1)
.PHONY: help copy init update up down status architect improve bootstrap register doctor

help: harness-help
copy: harness-copy
init: harness-init
update: harness-update
up: harness-up
down: harness-down
status: harness-status
architect: harness-architect
improve: harness-improve
bootstrap: harness-bootstrap
register: harness-register
doctor: harness-doctor
endif
