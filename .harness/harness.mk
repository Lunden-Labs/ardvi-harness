SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help
.NOTPARALLEL:

HARNESS_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
SCRIPTS_DIR := $(HARNESS_DIR)/scripts
HARNESS_PROJECT_ROOT := $(abspath $(HARNESS_DIR)/..)

.PHONY: help init update up down status architect improve bootstrap register doctor

help:
	@echo "Project harness"
	@echo "  make init       First-time project and CAO initialization"
	@echo "  make update     Update CAO, Addy skills, Agency Agents and Ponytail"
	@echo "  make up         Start the local CAO control plane"
	@echo "  make down       Stop all CAO sessions and the control plane"
	@echo "  make status     Show local CAO status"
	@echo "  make architect  Open the project architect in this terminal"
	@echo "  make improve    Ask Codex for one focused harness improvement"

init:
	@bash "$(SCRIPTS_DIR)/bootstrap.sh"
	@bash "$(SCRIPTS_DIR)/install.sh"
	@python3 "$(SCRIPTS_DIR)/register_cao.py"
	@bash "$(SCRIPTS_DIR)/doctor.sh"

update:
	@bash "$(SCRIPTS_DIR)/install.sh"
	@python3 "$(SCRIPTS_DIR)/register_cao.py"

up:
	@bash "$(SCRIPTS_DIR)/server.sh"

down:
	@bash "$(SCRIPTS_DIR)/down.sh"

status:
	@bash "$(SCRIPTS_DIR)/status.sh"

architect:
	@bash "$(SCRIPTS_DIR)/launch.sh"

improve:
	@codex -C "$(HARNESS_PROJECT_ROOT)" 'Read AGENTS.md and .harness/README.md first. Analyze the harness before editing. Propose and implement only one small, reviewable portability or safety improvement. Edit only .harness/** and necessary root harness or bootstrap documentation or Makefile entries. Do not edit product code, product documentation or configuration, dependencies, or generated files. Do not add secrets, absolute local paths, global-path coupling, or unrelated refactors. Do not commit or push. Run the narrowest relevant checks.'

bootstrap:
	@bash "$(SCRIPTS_DIR)/bootstrap.sh"

register:
	@python3 "$(SCRIPTS_DIR)/register_cao.py"

doctor:
	@bash "$(SCRIPTS_DIR)/doctor.sh"
