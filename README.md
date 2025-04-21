# Orchestration-Go

This project includes a local Go installation, so you don't need to install Go system-wide.

## Using the Local Go Installation

You can use the provided shell script to run Go commands:

```bash
# Run the hello world example
./go.sh run src/main.go

# Build a binary
./go.sh build -o bin/app src/main.go

# Get dependencies
./go.sh mod tidy

# Run any other Go command
./go.sh <command> [arguments]
```

## Project Structure

- `goroot/` - Contains the local Go installation
- `src/` - Source code
- `pkg/` - Package objects
- `bin/` - Compiled binaries

# CSV Processor API

A high-performance Go web service that processes CSV files, validates royalty percentages and date formats, and converts the data to JSON.

## Features

- **Fast Parallel Processing**: Configurable number of worker goroutines
- **Web Interface**: Easy-to-use upload form
- **REST API**: Simple endpoint for programmatic access
- **Validation**: Checks royalty percentages and date formats
- **Containerized**: Ready to deploy with Docker

## Quick Start with Docker

The easiest way to run the application is with Docker and Docker Compose:

```bash
# Build and start the service
make deploy

# Or manually:
docker-compose up -d
```

This will start the service on port 8080. Access the web interface at [http://localhost:8080](http://localhost:8080)

## Manual Docker Build

```bash
# Build the Docker image
docker build -t csv-processor-api .

# Run the container
docker run -p 8080:8080 --name csv-api csv-processor-api
```

## Using the Makefile

The project includes a Makefile with helpful commands:

```bash
# Show available commands
make help

# Build Docker image
make build

# Run Docker container
make run-docker

# Stop Docker container
make stop

# Clean up Docker resources
make clean
```

## API Usage

### Web Interface

Open [http://localhost:8080](http://localhost:8080) in your browser and use the upload form.

### API Endpoint

Use the `/upload` endpoint to programmatically process CSV files:

```bash
# Using curl
curl -X POST \
  -F "csvFile=@/path/to/your/file.csv" \
  -F "workers=4" \
  http://localhost:8080/upload > results.json
```

## Expected CSV Format

The CSV file should have the following headers:

```
Release ID, Release Title, Track ID, Track Title, ISRC, Artist Name, Genre,
Release Date, Label Name, UPC, Language, Explicit, Territories, Rights Holder,
File URL, Royalty Artist %, Royalty Label %, Royalty Distributor %, Royalty Publisher %
```

## Response Format

The API returns a JSON object with two main sections:

1. `validation`: Validation results for each row, keyed by Track ID
2. `conversion`: The converted CSV data as an array of objects

Example:

```json
{
  "validation": {
    "TRK001": {
      "release_id": "RLS001",
      "track_id": "TRK001",
      "royalties_sum": true,
      "date_format": true
    },
    ...
  },
  "conversion": [
    {
      "Release ID": "RLS001",
      "Release Title": "Midnight Drive",
      ...
    },
    ...
  ]
}
```

## Building Without Docker

If you have Go installed locally (version 1.22 or later), you can build and run without Docker:

```bash
# Using the local go.sh script (if available)
./go.sh run src/main.go

# Or using the standard Go command
go run src/main.go

# Build a binary
go build -o bin/csvapi src/main.go
./bin/csvapi
```

## Deployment

The Docker image is ready for deployment to various cloud platforms:

- **AWS ECS/Fargate**: Use the provided Dockerfile
- **Google Cloud Run**: Deploy directly from the Docker image
- **Kubernetes**: Use the provided Docker image with your K8s configuration
- **Digital Ocean App Platform**: Deploy directly from the Docker image

## Environment Variables

- `PORT`: The port on which the server will listen (default: 8080)

## Testing with Sample Data

The repository includes sample CSV files for testing:

- `src/sample-data/sample.csv`: A clean sample with valid data
- `src/sample-data/sample-with-errors.csv`: A sample with validation errors to test error handling

You can use these files to test the application:

```bash
# Upload via web interface
# Navigate to http://localhost:8080 and select one of the sample files

# Or using curl
curl -X POST \
  -F "csvFile=@src/sample-data/sample.csv" \
  -F "workers=4" \
  http://localhost:8080/upload > results.json

# Test with invalid data
curl -X POST \
  -F "csvFile=@src/sample-data/sample-with-errors.csv" \
  -F "workers=4" \
  http://localhost:8080/upload > error_results.json
```

## License

[MIT License](LICENSE)
