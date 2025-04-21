.PHONY: build run-docker stop clean docker-compose-up docker-compose-down

# Docker image name
IMAGE_NAME=csv-processor-api

# Build the Docker image
build:
	docker build -t $(IMAGE_NAME) .

# Run the Docker container
run-docker:
	docker run -p 8080:8080 --name csv-api $(IMAGE_NAME)

# Stop the Docker container
stop:
	docker stop csv-api || true
	docker rm csv-api || true

# Clean Docker resources
clean: stop
	docker rmi $(IMAGE_NAME) || true

# Run with Docker Compose
docker-compose-up:
	docker-compose up -d

# Stop Docker Compose services
docker-compose-down:
	docker-compose down

# Build and run with Docker Compose
deploy: docker-compose-down docker-compose-up

# Build Go application locally (if Go is installed)
build-local:
	cd src && go build -o ../bin/csvapi main.go

# Run Go application locally (if Go is installed)
run-local:
	cd src && go run main.go

# Show help
help:
	@echo "Available targets:"
	@echo "  build            - Build the Docker image"
	@echo "  run-docker       - Run the Docker container"
	@echo "  stop             - Stop the Docker container"
	@echo "  clean            - Remove the Docker container and image"
	@echo "  docker-compose-up - Start services with Docker Compose"
	@echo "  docker-compose-down - Stop services with Docker Compose"
	@echo "  deploy           - Rebuild and restart with Docker Compose"
	@echo "  build-local      - Build the Go application locally"
	@echo "  run-local        - Run the Go application locally" 