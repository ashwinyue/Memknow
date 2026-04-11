.PHONY: fs setup-lark-bot build run daemon ds dr dsp daemon-install daemon-uninstall daemon-start daemon-stop daemon-restart daemon-status

SETUP_LARK_BOT_SCRIPT ?= ./setup_lark_bot.sh
DAEMON_SCRIPT ?= ./daemon-mac.sh
CONFIG ?= ./config.yaml
DAEMON_BINARY ?= $(CURDIR)/server
DAEMON_WORK_DIR ?= $(CURDIR)
APP_ID ?=
WORKSPACE ?=
TEMPLATE ?=

fs:
	@APP_ID_INPUT="$(APP_ID)"; \
	if [ -z "$$APP_ID_INPUT" ]; then \
		printf "APP_ID: "; \
		read APP_ID_INPUT; \
	fi; \
	if [ -z "$$APP_ID_INPUT" ]; then \
		echo "APP_ID is required"; \
		exit 1; \
	fi; \
	TEMPLATE_INPUT="$(TEMPLATE)"; \
	if [ -z "$$TEMPLATE_INPUT" ]; then \
		echo "Select workspace template:"; \
		echo "  1. default"; \
		echo "  2. product-assistant"; \
		echo "  3. code-review"; \
		printf "Choice (default: 1): "; \
		read TEMPLATE_INPUT; \
	fi; \
	if [ -z "$$TEMPLATE_INPUT" ]; then \
		TEMPLATE_INPUT="1"; \
	fi; \
	case "$$TEMPLATE_INPUT" in \
		1) TEMPLATE_INPUT="default" ;; \
		2) TEMPLATE_INPUT="product-assistant" ;; \
		3) TEMPLATE_INPUT="code-review" ;; \
	esac; \
	case "$$TEMPLATE_INPUT" in \
		default|product-assistant|code-review) ;; \
		*) echo "TEMPLATE must be one of: default, product-assistant, code-review"; exit 1 ;; \
	esac; \
	WORKSPACE_INPUT="$(WORKSPACE)"; \
	if [ -z "$$WORKSPACE_INPUT" ]; then \
		if [ "$$TEMPLATE_INPUT" = "code-review" ]; then \
			WORKSPACE_INPUT="/Users/mervyn/GolandProjects/github/code-review-demo/code"; \
		else \
			WORKSPACE_INPUT="./workspaces/$$APP_ID_INPUT"; \
		fi; \
	fi; \
	"$(SETUP_LARK_BOT_SCRIPT)" "$$APP_ID_INPUT" "$$WORKSPACE_INPUT" "$$TEMPLATE_INPUT"

setup-lark-bot: fs

build:
	go build -o server ./cmd/server

run:
	go run ./cmd/server -config "$(CONFIG)"

daemon: daemon-install

daemon-install:
	@chmod +x "$(DAEMON_SCRIPT)"
	"$(DAEMON_SCRIPT)" install --binary "$(DAEMON_BINARY)" --work-dir "$(DAEMON_WORK_DIR)" --config "$(CONFIG)"

daemon-uninstall:
	"$(DAEMON_SCRIPT)" uninstall

daemon-start ds:
	"$(DAEMON_SCRIPT)" start

daemon-stop dsp:
	"$(DAEMON_SCRIPT)" stop

daemon-restart dr: build
	@echo "Killing process on port 8786..."
	@lsof -ti:8786 | xargs kill -9 2>/dev/null || true
	"$(DAEMON_SCRIPT)" restart

daemon-status:
	"$(DAEMON_SCRIPT)" status
