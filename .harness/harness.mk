ARDVI_HARNESS_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
ARDVI_HARNESS_SCRIPTS_DIR := $(ARDVI_HARNESS_DIR)/scripts
ARDVI_HARNESS_PROJECT_ROOT := $(abspath $(ARDVI_HARNESS_DIR)/..)
export PROMPT PROMPT_FILE

.PHONY: harness-help harness-copy harness-init harness-update harness-up harness-down harness-status harness-skills harness-upstream-lock harness-improve harness-bootstrap harness-doctor harness-skill-path harness-memory-export harness-memory-import

harness-help:
	@echo "ARDVI harness"
	@echo "  make harness-copy [TARGET=/path]  Copy harness into a Git root"
	@echo "  make harness-init [PROMPT='...']  Initialize native Codex/Claude integration"
	@echo "  make harness-update     Update harness, MCP image, and every managed skill"
	@echo "  make harness-up         Ensure the machine-wide Ardvi MCP service is running"
	@echo "  make harness-down       Stop the machine-wide service (affects all projects)"
	@echo "  make harness-status     Show hub status and installed revisions"
	@echo "  make harness-skills     List skills installed on the MCP server"
	@echo "  make harness-upstream-lock  Maintainer: refresh pinned skill revisions"
	@echo "  make harness-memory-export OUTPUT=.ardvi/memory.jsonl"
	@echo "  make harness-memory-import INPUT=.ardvi/memory.jsonl"
	@echo "  make harness-improve    Ask Codex for one focused harness improvement"
	@echo "  make harness-doctor     Validate harness dependencies and registration"
	@echo "  make harness-skill-path SKILL=name  Locate communication or a writing skill"

harness-copy:
	@TARGET="$(TARGET)" bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/copy.sh"

harness-init:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/bootstrap.sh"
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/install.sh"
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/doctor.sh"

harness-update:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/update_harness.sh"
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/bootstrap.sh"
	@ardvi update
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/doctor.sh"

harness-up:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/server.sh"

harness-down:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/down.sh"

harness-status:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/status.sh"

harness-skills:
	@ardvi skills list

harness-upstream-lock:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/update_upstream_lock.sh"

harness-improve:
	@codex -C "$(ARDVI_HARNESS_PROJECT_ROOT)" 'Read AGENTS.md and .harness/README.md first. Analyze the harness before editing. Propose and implement only one small, reviewable portability or safety improvement. Edit only .harness/** and necessary root harness or bootstrap documentation or Makefile entries. Do not edit product code, product documentation or configuration, dependencies, or generated files. Do not add secrets, absolute local paths, global-path coupling, or unrelated refactors. Do not commit or push. Run the narrowest relevant checks.'

harness-bootstrap:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/bootstrap.sh"

harness-doctor:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/doctor.sh"

harness-skill-path:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/skill_path.sh" "$(SKILL)"

harness-memory-export:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/memory.sh" export "$(OUTPUT)"

harness-memory-import:
	@bash "$(ARDVI_HARNESS_SCRIPTS_DIR)/memory.sh" import "$(INPUT)"

ifeq ($(ARDVI_HARNESS_SHORT_TARGETS),1)
.PHONY: help copy init update up down status skills improve bootstrap doctor skill-path memory-export memory-import

help: harness-help
copy: harness-copy
init: harness-init
update: harness-update
up: harness-up
down: harness-down
status: harness-status
skills: harness-skills
improve: harness-improve
bootstrap: harness-bootstrap
doctor: harness-doctor
skill-path: harness-skill-path
memory-export: harness-memory-export
memory-import: harness-memory-import
endif
