GO_MODULES = . ./client

.PHONY: lint test testacc docs tidy

lint:
	golangci-lint run ./...
	cd client && golangci-lint run ./...

tidy:
	go mod tidy
	cd client && go mod tidy

test:
	# root module first: cheap and hermetic (internal/acctest gates on TF_ACC), fail fast
	go test ./... -timeout 5m
	# -p 1: toxiproxy fault-injection tests race under cross-package parallelism (two docker stacks)
	cd client && go test ./... -v -timeout 20m -p 1

testacc:
	TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org \
	TF_ACC_TERRAFORM_PATH=$$(command -v tofu) \
	go test ./internal/acctest/... -v -timeout 45m

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0 \
	  generate --provider-name leifwind
