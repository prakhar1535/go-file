package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Record represents a row from the CSV file
type Record struct {
	ReleaseID              string `json:"Release ID"`
	ReleaseTitle           string `json:"Release Title"`
	TrackID                string `json:"Track ID"`
	TrackTitle             string `json:"Track Title"`
	ISRC                   string `json:"ISRC"`
	ArtistName             string `json:"Artist Name"`
	Genre                  string `json:"Genre"`
	ReleaseDate            string `json:"Release Date"`
	LabelName              string `json:"Label Name"`
	UPC                    string `json:"UPC"`
	Language               string `json:"Language"`
	Explicit               string `json:"Explicit"`
	Territories            string `json:"Territories"`
	RightsHolder           string `json:"Rights Holder"`
	FileURL                string `json:"File URL"`
	RoyaltyArtistPercent   string `json:"Royalty Artist %"`
	RoyaltyLabelPercent    string `json:"Royalty Label %"`
	RoyaltyDistPercent     string `json:"Royalty Distributor %"`
	RoyaltyPublisherPercent string `json:"Royalty Publisher %"`
}

// RowValidation represents the validation results for a single row
type RowValidation struct {
	ReleaseID    string `json:"release_id"`
	TrackID      string `json:"track_id"`
	RoyaltiesSum bool   `json:"royalties_sum"`
	DateFormat   bool   `json:"date_format"`
}

// OutputFormat represents the final output format
type OutputFormat struct {
	Validation map[string]RowValidation `json:"validation"`
	Conversion []map[string]string      `json:"conversion"`
}

// parsePercentage parses a string like "50%" to a float64
func parsePercentage(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	return strconv.ParseFloat(s, 64)
}

// processCSV processes the CSV file and returns the validation results
func processCSV(file multipart.File, numWorkers int) (*OutputFormat, error) {
	reader := csv.NewReader(file)
	
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %v", err)
	}

	type result struct {
		Data       map[string]string
		Validation RowValidation
	}

	batchSize := 1000
	rowsChan := make(chan []string, batchSize)
	resultsChan := make(chan result, batchSize)
	
	var wg sync.WaitGroup
	
	// Date format regex (YYYY-MM-DD)
	dateRegex := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	
	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range rowsChan {
				// Create a map for the row data
				recordMap := make(map[string]string)
				for i, value := range row {
					if i < len(headers) {
						recordMap[headers[i]] = value
					}
				}

				// Initialize validation for this row
				validation := RowValidation{
					ReleaseID:    recordMap["Release ID"],
					TrackID:      recordMap["Track ID"],
					RoyaltiesSum: true,
					DateFormat:   true,
				}

				// Validate royalty percentages
				artistPct, labelPct, distPct, pubPct := 0.0, 0.0, 0.0, 0.0
				
				if pct, err := parsePercentage(recordMap["Royalty Artist %"]); err == nil {
					artistPct = pct
				}
				
				if pct, err := parsePercentage(recordMap["Royalty Label %"]); err == nil {
					labelPct = pct
				}
				
				if pct, err := parsePercentage(recordMap["Royalty Distributor %"]); err == nil {
					distPct = pct
				}
				
				if pct, err := parsePercentage(recordMap["Royalty Publisher %"]); err == nil {
					pubPct = pct
				}
				
				sum := artistPct + labelPct + distPct + pubPct
				if sum != 100.0 && (sum < 99.9 || sum > 100.1) {
					validation.RoyaltiesSum = false
				}
				
				// Validate date format
				releaseDate := recordMap["Release Date"]
				if !dateRegex.MatchString(releaseDate) {
					validation.DateFormat = false
				}
				
				resultsChan <- result{
					Data:       recordMap,
					Validation: validation,
				}
			}
		}()
	}
	
	// Start a goroutine to close resultsChan when all workers are done
	go func() {
		wg.Wait()
		close(resultsChan)
	}()
	
	// Read and process rows in batches
	var count int
	go func() {
		for {
			row, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Printf("Error reading row: %s", err)
				continue
			}
			
			rowsChan <- row
			count++
		}
		close(rowsChan)
	}()
	
	// Collect all results
	var records []map[string]string
	validations := make(map[string]RowValidation)
	
	for result := range resultsChan {
		records = append(records, result.Data)
		// Use TrackID as the key for validations
		validations[result.Validation.TrackID] = result.Validation
	}
	
	// Create final output structure
	outputData := &OutputFormat{
		Validation: validations,
		Conversion: records,
	}
	
	return outputData, nil
}

// uploadHandler handles the CSV file upload
func uploadHandler(w http.ResponseWriter, r *http.Request) {
	// Only allow POST method
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form with 32MB max memory
	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get the uploaded file
	file, header, err := r.FormFile("csvFile")
	if err != nil {
		http.Error(w, "Failed to get file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Check if the file is a CSV
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".csv") {
		http.Error(w, "Only CSV files are allowed", http.StatusBadRequest)
		return
	}

	// Get the number of workers
	numWorkersStr := r.FormValue("workers")
	numWorkers := runtime.NumCPU() // Default to number of CPU cores
	if numWorkersStr != "" {
		parsedWorkers, err := strconv.Atoi(numWorkersStr)
		if err == nil && parsedWorkers > 0 {
			numWorkers = parsedWorkers
		}
	}

	// Process the CSV file
	result, err := processCSV(file, numWorkers)
	if err != nil {
		http.Error(w, "Failed to process CSV: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the results as JSON
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		http.Error(w, "Failed to encode results: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// indexHandler serves the upload form
func indexHandler(w http.ResponseWriter, r *http.Request) {
	html := `
<!DOCTYPE html>
<html>
<head>
    <title>CSV Processor</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            max-width: 800px;
            margin: 0 auto;
            padding: 20px;
        }
        .form-group {
            margin-bottom: 15px;
        }
        label {
            display: block;
            margin-bottom: 5px;
        }
        .btn {
            background-color: #4CAF50;
            color: white;
            padding: 10px 15px;
            border: none;
            border-radius: 4px;
            cursor: pointer;
        }
    </style>
</head>
<body>
    <h1>CSV Processor</h1>
    <p>Upload a CSV file to process it and validate royalty percentages and date formats.</p>
    
    <form action="/upload" method="post" enctype="multipart/form-data">
        <div class="form-group">
            <label for="csvFile">CSV File:</label>
            <input type="file" id="csvFile" name="csvFile" accept=".csv" required>
        </div>
        
        <div class="form-group">
            <label for="workers">Number of Workers (default is number of CPU cores):</label>
            <input type="number" id="workers" name="workers" min="1" value="` + strconv.Itoa(runtime.NumCPU()) + `">
        </div>
        
        <button type="submit" class="btn">Process CSV</button>
    </form>
</body>
</html>
`
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, html)
}

func main() {
	// Define API routes
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/upload", uploadHandler)

	// Read port from environment variable or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Start the server
	fmt.Printf("Server starting on port %s...\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
} 