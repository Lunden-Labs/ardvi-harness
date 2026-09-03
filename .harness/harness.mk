SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help
.NOTPARALLEL:

HARNESS_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
SCRIPTS_DIR := $(HARNESS_DIR)/scripts

.PHONY: help init update up down status architect bootstrap register doctor

help:
	@echo "Project harness"
	@echo "  make init       First-time project and CAO initialization"
	@echo "  make update     Update CAO, Addy skills, Agency Agents and Ponytail"
	@echo "  make up         Start the local CAO control plane"
	@echo "  make down       Stop all CAO sessions and the control plane"
	@echo "  make status     Show local CAO status"
	@echo "  make architect  Open the project architect in this terminal"

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

bootstrap:
	@bash "$(SCRIPTS_DIR)/bootstrap.sh"

register:
	@python3 "$(SCRIPTS_DIR)/register_cao.py"

doctor:
	@bash "$(SCRIPTS_DIR)/doctor.sh"
