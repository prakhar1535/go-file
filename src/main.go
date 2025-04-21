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
	"time"
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

// WorkerStatus represents the current status of a worker goroutine
type WorkerStatus struct {
	ID            int       `json:"id"`
	Active        bool      `json:"active"`
	ProcessedRows int       `json:"processed_rows"`
	CurrentRow    string    `json:"current_row,omitempty"`
	StartTime     time.Time `json:"start_time"`
	LastUpdate    time.Time `json:"last_update"`
}

// Global variables to track worker status
var (
	workerStatuses = make(map[int]*WorkerStatus)
	statusMutex    sync.RWMutex
	activeJob      bool
	activeJobMutex sync.RWMutex
)

// parsePercentage parses a string like "50%" to a float64
func parsePercentage(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	return strconv.ParseFloat(s, 64)
}

// processCSV processes the CSV file and returns the validation results
func processCSV(file multipart.File, numWorkers int) (*OutputFormat, error) {
	// Reset worker statuses when starting a new job
	statusMutex.Lock()
	workerStatuses = make(map[int]*WorkerStatus)
	statusMutex.Unlock()
	
	// Set active job flag
	activeJobMutex.Lock()
	activeJob = true
	activeJobMutex.Unlock()
	
	defer func() {
		// Mark job as inactive when done
		activeJobMutex.Lock()
		activeJob = false
		activeJobMutex.Unlock()
		
		// Explicitly mark all workers as inactive when job completes
		statusMutex.Lock()
		for _, worker := range workerStatuses {
			worker.Active = false
			worker.LastUpdate = time.Now()
			worker.CurrentRow = ""
		}
		statusMutex.Unlock()
	}()

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
		
		// Initialize worker status
		workerID := i
		statusMutex.Lock()
		workerStatuses[workerID] = &WorkerStatus{
			ID:        workerID,
			Active:    true,
			StartTime: time.Now(),
			LastUpdate: time.Now(),
		}
		statusMutex.Unlock()
		
		go func() {
			defer wg.Done()
			
			// Cleanup worker status when done
			defer func() {
				statusMutex.Lock()
				if ws, exists := workerStatuses[workerID]; exists {
					ws.Active = false
					ws.LastUpdate = time.Now()
				}
				statusMutex.Unlock()
			}()
			
			for row := range rowsChan {
				// Update worker status
				statusMutex.Lock()
				if ws, exists := workerStatuses[workerID]; exists {
					ws.ProcessedRows++
					if len(row) > 0 {
						ws.CurrentRow = row[0] // First column (Release ID)
					}
					ws.LastUpdate = time.Now()
				}
				statusMutex.Unlock()
				
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

	// Set CORS headers for AJAX requests
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

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

// statusHandler returns the current status of worker goroutines
func statusHandler(w http.ResponseWriter, r *http.Request) {
	// Get active job status
	activeJobMutex.RLock()
	isActive := activeJob
	activeJobMutex.RUnlock()
	
	// Read worker statuses
	statusMutex.RLock()
	statuses := make([]*WorkerStatus, 0, len(workerStatuses))
	for _, status := range workerStatuses {
		// Create a copy to avoid race conditions
		statusCopy := *status
		statuses = append(statuses, &statusCopy)
	}
	statusMutex.RUnlock()
	
	// Create response
	response := struct {
		JobActive bool           `json:"job_active"`
		Workers   []*WorkerStatus `json:"workers"`
	}{
		JobActive: isActive,
		Workers:   statuses,
	}
	
	// Return as JSON
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		http.Error(w, "Failed to encode status: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// indexHandler serves the upload form with worker visualization
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
        #status-container {
            margin-top: 20px;
            padding: 10px;
            border: 1px solid #ddd;
            border-radius: 4px;
        }
        .workers-grid {
            display: flex;
            flex-wrap: wrap;
            gap: 10px;
            margin-top: 15px;
        }
        .worker-card {
            border: 1px solid #ccc;
            border-radius: 5px;
            padding: 10px;
            width: 180px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .worker-active {
            background-color: #e8f5e9;
            border-color: #4CAF50;
        }
        .worker-idle {
            background-color: #f5f5f5;
        }
        .worker-header {
            display: flex;
            justify-content: space-between;
            margin-bottom: 8px;
            font-weight: bold;
        }
        .worker-body {
            font-size: 14px;
        }
        .status-indicator {
            display: inline-block;
            width: 10px;
            height: 10px;
            border-radius: 50%;
            margin-right: 5px;
        }
        .status-active {
            background-color: #4CAF50;
        }
        .status-idle {
            background-color: #9e9e9e;
        }
        .job-status {
            font-weight: bold;
            padding: 8px;
            margin-bottom: 10px;
            border-radius: 4px;
            text-align: center;
        }
        .job-active {
            background-color: #e8f5e9;
            color: #2e7d32;
        }
        .job-idle {
            background-color: #f5f5f5;
            color: #616161;
        }
        .stats {
            margin-top: 5px;
            display: flex;
            flex-direction: column;
            gap: 3px;
        }
        #results-container {
            margin-top: 20px;
            display: none;
            border: 1px solid #ddd;
            border-radius: 4px;
            padding: 15px;
        }
        .tab-container {
            margin-top: 10px;
        }
        .tab {
            overflow: hidden;
            border: 1px solid #ccc;
            background-color: #f1f1f1;
            border-radius: 4px 4px 0 0;
        }
        .tab button {
            background-color: inherit;
            float: left;
            border: none;
            outline: none;
            cursor: pointer;
            padding: 10px 16px;
            transition: 0.3s;
            font-size: 14px;
        }
        .tab button:hover {
            background-color: #ddd;
        }
        .tab button.active {
            background-color: #4CAF50;
            color: white;
        }
        .tabcontent {
            display: none;
            padding: 12px;
            border: 1px solid #ccc;
            border-top: none;
            border-radius: 0 0 4px 4px;
            max-height: 400px;
            overflow: auto;
        }
        .validation-summary {
            margin: 10px 0;
            padding: 10px;
            border-radius: 4px;
        }
        .validation-success {
            background-color: #e8f5e9;
            color: #2e7d32;
        }
        .validation-errors {
            background-color: #ffebee;
            color: #c62828;
        }
        table {
            width: 100%;
            border-collapse: collapse;
        }
        table, th, td {
            border: 1px solid #ddd;
        }
        th, td {
            padding: 8px;
            text-align: left;
        }
        th {
            background-color: #f2f2f2;
        }
        tr:nth-child(even) {
            background-color: #f9f9f9;
        }
        pre {
            white-space: pre-wrap;
            word-wrap: break-word;
        }
        .loading {
            display: none;
            text-align: center;
            margin: 20px 0;
        }
        .spinner {
            border: 4px solid #f3f3f3;
            border-top: 4px solid #4CAF50;
            border-radius: 50%;
            width: 30px;
            height: 30px;
            animation: spin 2s linear infinite;
            margin: 0 auto;
        }
        @keyframes spin {
            0% { transform: rotate(0deg); }
            100% { transform: rotate(360deg); }
        }
    </style>
</head>
<body>
    <h1>CSV Processor</h1>
    <p>Upload a CSV file to process it and validate royalty percentages and date formats.</p>
    
    <form id="upload-form">
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
    
    <div id="loading" class="loading">
        <div class="spinner"></div>
        <p>Processing file, please wait...</p>
    </div>
    
    <div id="status-container">
        <h2>Worker Status</h2>
        <div id="job-status" class="job-status job-idle">No active job</div>
        <div id="workers-grid" class="workers-grid">
            <div class="worker-card worker-idle">
                <div class="worker-header">
                    <span>Worker #0</span>
                    <span class="status-indicator status-idle"></span>
                </div>
                <div class="worker-body">
                    <div class="stats">
                        <div>Processed: 0 rows</div>
                        <div>Current: None</div>
                    </div>
                </div>
            </div>
        </div>
    </div>
    
    <div id="results-container">
        <h2>Results</h2>
        <div class="validation-summary" id="validation-summary"></div>
        
        <div class="tab-container">
            <div class="tab">
                <button class="tablinks" onclick="openTab(event, 'validation-tab')" id="defaultOpen">Validation</button>
                <button class="tablinks" onclick="openTab(event, 'data-tab')">Data</button>
                <button class="tablinks" onclick="openTab(event, 'json-tab')">Raw JSON</button>
            </div>
            
            <div id="validation-tab" class="tabcontent">
                <table id="validation-table">
                    <thead>
                        <tr>
                            <th>Track ID</th>
                            <th>Release ID</th>
                            <th>Royalties Sum</th>
                            <th>Date Format</th>
                        </tr>
                    </thead>
                    <tbody id="validation-body">
                    </tbody>
                </table>
            </div>
            
            <div id="data-tab" class="tabcontent">
                <table id="data-table">
                    <thead id="data-head">
                    </thead>
                    <tbody id="data-body">
                    </tbody>
                </table>
            </div>
            
            <div id="json-tab" class="tabcontent">
                <pre id="json-output"></pre>
            </div>
        </div>
    </div>
    
    <script>
        // Function to update worker status
        function updateWorkerStatus(forceComplete = false) {
            fetch('/status')
                .then(response => response.json())
                .then(data => {
                    // Update job status
                    const jobStatusEl = document.getElementById('job-status');
                    const statusContainer = document.getElementById('status-container');
                    
                    if (data.job_active && !forceComplete) {
                        jobStatusEl.textContent = 'Job is active - processing file';
                        jobStatusEl.className = 'job-status job-active';
                        statusContainer.style.borderColor = '#4CAF50';
                    } else {
                        jobStatusEl.textContent = 'No active job';
                        jobStatusEl.className = 'job-status job-idle';
                        statusContainer.style.borderColor = '#ddd';
                        
                        // If we're displaying results, add a message
                        if (document.getElementById('results-container').style.display === 'block') {
                            jobStatusEl.textContent = 'Processing complete';
                        }
                    }
                    
                    // Update workers grid
                    const workersGrid = document.getElementById('workers-grid');
                    
                    // If job is complete and we're showing results, consider hiding the worker grid
                    if (!data.job_active && document.getElementById('results-container').style.display === 'block') {
                        // Option 1: Hide the worker grid
                        // workersGrid.style.display = 'none';
                        
                        // Option 2: Show workers in idle state
                        workersGrid.innerHTML = '';
                        
                        data.workers.forEach(worker => {
                            const workerEl = document.createElement('div');
                            workerEl.className = 'worker-card worker-idle';
                            
                            const workerHeader = document.createElement('div');
                            workerHeader.className = 'worker-header';
                            
                            const workerTitle = document.createElement('span');
                            workerTitle.textContent = 'Worker #' + worker.id;
                            
                            const statusIndicator = document.createElement('span');
                            statusIndicator.className = 'status-indicator status-idle';
                            
                            workerHeader.appendChild(workerTitle);
                            workerHeader.appendChild(statusIndicator);
                            
                            const workerBody = document.createElement('div');
                            workerBody.className = 'worker-body';
                            
                            const stats = document.createElement('div');
                            stats.className = 'stats';
                            
                            const processed = document.createElement('div');
                            processed.textContent = 'Processed: ' + worker.processed_rows + ' rows';
                            
                            const current = document.createElement('div');
                            current.textContent = 'Current: None';
                            
                            stats.appendChild(processed);
                            stats.appendChild(current);
                            
                            workerBody.appendChild(stats);
                            
                            workerEl.appendChild(workerHeader);
                            workerEl.appendChild(workerBody);
                            
                            workersGrid.appendChild(workerEl);
                        });
                    } else if (data.job_active || !document.getElementById('results-container').style.display === 'block') {
                        // Normal update for active jobs or when results aren't showing
                        workersGrid.innerHTML = '';
                        
                        data.workers.forEach(worker => {
                            const workerEl = document.createElement('div');
                            workerEl.className = worker.active ? 'worker-card worker-active' : 'worker-card worker-idle';
                            
                            const workerHeader = document.createElement('div');
                            workerHeader.className = 'worker-header';
                            
                            const workerTitle = document.createElement('span');
                            workerTitle.textContent = 'Worker #' + worker.id;
                            
                            const statusIndicator = document.createElement('span');
                            statusIndicator.className = worker.active ? 
                                'status-indicator status-active' : 
                                'status-indicator status-idle';
                            
                            workerHeader.appendChild(workerTitle);
                            workerHeader.appendChild(statusIndicator);
                            
                            const workerBody = document.createElement('div');
                            workerBody.className = 'worker-body';
                            
                            const stats = document.createElement('div');
                            stats.className = 'stats';
                            
                            const processed = document.createElement('div');
                            processed.textContent = 'Processed: ' + worker.processed_rows + ' rows';
                            
                            const current = document.createElement('div');
                            current.textContent = 'Current: ' + (worker.current_row || 'None');
                            
                            stats.appendChild(processed);
                            stats.appendChild(current);
                            
                            workerBody.appendChild(stats);
                            
                            workerEl.appendChild(workerHeader);
                            workerEl.appendChild(workerBody);
                            
                            workersGrid.appendChild(workerEl);
                        });
                    }
                })
                .catch(error => {
                    console.error('Error fetching worker status:', error);
                });
        }
        
        // Tab functionality
        function openTab(evt, tabName) {
            var i, tabcontent, tablinks;
            tabcontent = document.getElementsByClassName("tabcontent");
            for (i = 0; i < tabcontent.length; i++) {
                tabcontent[i].style.display = "none";
            }
            tablinks = document.getElementsByClassName("tablinks");
            for (i = 0; i < tablinks.length; i++) {
                tablinks[i].className = tablinks[i].className.replace(" active", "");
            }
            document.getElementById(tabName).style.display = "block";
            evt.currentTarget.className += " active";
        }
        
        // Display results in the UI
        function displayResults(data) {
            document.getElementById('loading').style.display = 'none';
            document.getElementById('results-container').style.display = 'block';
            
            // Force one final status update to show all workers as inactive
            updateWorkerStatus(true);
            
            // Display raw JSON
            document.getElementById('json-output').textContent = JSON.stringify(data, null, 2);
            
            // Process validation data
            const validationBody = document.getElementById('validation-body');
            validationBody.innerHTML = '';
            
            let allValid = true;
            let validationCount = 0;
            
            for (const [trackId, validation] of Object.entries(data.validation)) {
                validationCount++;
                const tr = document.createElement('tr');
                
                const tdTrackId = document.createElement('td');
                tdTrackId.textContent = trackId;
                tr.appendChild(tdTrackId);
                
                const tdReleaseId = document.createElement('td');
                tdReleaseId.textContent = validation.release_id;
                tr.appendChild(tdReleaseId);
                
                const tdRoyalties = document.createElement('td');
                tdRoyalties.textContent = validation.royalties_sum ? '✓' : '✗';
                tdRoyalties.style.color = validation.royalties_sum ? 'green' : 'red';
                tr.appendChild(tdRoyalties);
                
                const tdDate = document.createElement('td');
                tdDate.textContent = validation.date_format ? '✓' : '✗';
                tdDate.style.color = validation.date_format ? 'green' : 'red';
                tr.appendChild(tdDate);
                
                validationBody.appendChild(tr);
                
                if (!validation.royalties_sum || !validation.date_format) {
                    allValid = false;
                }
            }
            
            // Validation summary
            const summaryEl = document.getElementById('validation-summary');
            if (allValid) {
                summaryEl.textContent = "All " + validationCount + " rows passed validation.";
                summaryEl.className = 'validation-summary validation-success';
            } else {
                summaryEl.textContent = "Some rows failed validation. Check the Validation tab for details.";
                summaryEl.className = 'validation-summary validation-errors';
            }
            
            // Process data
            if (data.conversion && data.conversion.length > 0) {
                const firstRow = data.conversion[0];
                const headers = Object.keys(firstRow);
                
                // Set table headers
                const dataHead = document.getElementById('data-head');
                const headerRow = document.createElement('tr');
                headers.forEach(header => {
                    const th = document.createElement('th');
                    th.textContent = header;
                    headerRow.appendChild(th);
                });
                dataHead.innerHTML = '';
                dataHead.appendChild(headerRow);
                
                // Set table body
                const dataBody = document.getElementById('data-body');
                dataBody.innerHTML = '';
                
                data.conversion.forEach(row => {
                    const tr = document.createElement('tr');
                    headers.forEach(header => {
                        const td = document.createElement('td');
                        td.textContent = row[header];
                        tr.appendChild(td);
                    });
                    dataBody.appendChild(tr);
                });
            }
            
            // Open default tab
            document.getElementById('defaultOpen').click();
        }
        
        // Form submission handling
        document.getElementById('upload-form').addEventListener('submit', function(e) {
            e.preventDefault();
            
            const formData = new FormData(this);
            const loadingEl = document.getElementById('loading');
            
            // Reset results container
            document.getElementById('results-container').style.display = 'none';
            
            // Show loading indicator
            loadingEl.style.display = 'block';
            
            // Change job status to starting
            const jobStatusEl = document.getElementById('job-status');
            jobStatusEl.textContent = 'Starting job...';
            jobStatusEl.className = 'job-status job-active';
            
            // Clear any existing interval and set up a more frequent update during processing
            if (window.statusInterval) {
                clearInterval(window.statusInterval);
            }
            window.statusInterval = setInterval(updateWorkerStatus, 500);
            
            // Send the form data to the server
            fetch('/upload', {
                method: 'POST',
                body: formData
            })
            .then(response => {
                if (!response.ok) {
                    throw new Error('Server error: ' + response.status);
                }
                return response.json();
            })
            .then(data => {
                // Stop frequent updates
                clearInterval(window.statusInterval);
                
                // Display the results
                displayResults(data);
                
                // Return to normal update frequency, but less frequent when complete
                window.statusInterval = setInterval(updateWorkerStatus, 2000);
            })
            .catch(error => {
                console.error('Error:', error);
                loadingEl.style.display = 'none';
                alert('Error processing file: ' + error.message);
                
                // Return to normal update frequency
                clearInterval(window.statusInterval);
                window.statusInterval = setInterval(updateWorkerStatus, 1000);
            });
        });
        
        // Update status every second
        window.statusInterval = setInterval(updateWorkerStatus, 1000);
        
        // Initial update
        updateWorkerStatus();
    </script>
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
	http.HandleFunc("/status", statusHandler)

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