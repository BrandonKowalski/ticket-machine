package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/stianeikeland/go-rpio/v4"
)

var (
	dispenserPin rpio.Pin
	sensorPin    rpio.Pin
	mutex        sync.Mutex
	isDispensing bool
	status       string
)

type StatusResponse struct {
	Status       string `json:"status"`
	IsDispensing bool   `json:"isDispensing"`
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "localhost"
	}

	for _, address := range addrs {
		// Check the address type and if it is not a loopback then display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "localhost"
}

func main() {
	if err := rpio.Open(); err != nil {
		fmt.Println("Error opening GPIO:", err)
		os.Exit(1)
	}
	defer rpio.Close()

	dispenserPin = rpio.Pin(18)
	sensorPin = rpio.Pin(17)

	dispenserPin.Output()
	sensorPin.Input()
	sensorPin.PullUp()

	fmt.Println("GPIO initialized successfully!")

	fmt.Println("Starting web server for ticket dispenser control...")

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	http.HandleFunc("/api/dispense", dispenseHandler)
	http.HandleFunc("/api/status", statusHandler)

	if _, err := os.Stat("./static"); os.IsNotExist(err) {
		os.Mkdir("./static", 0755)
	}

	createStaticFiles()

	localIP := getLocalIP()
	port := "8080"

	fmt.Printf("Web server started at http://%s:%s\n", localIP, port)
	fmt.Println("Use this address to access the ticket dispenser from other devices on your network")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func dispenseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	numStr := r.FormValue("tickets")
	numTickets, err := strconv.Atoi(numStr)
	if err != nil || numTickets <= 0 {
		http.Error(w, "Invalid number of tickets", http.StatusBadRequest)
		return
	}

	// Check if already dispensing
	mutex.Lock()
	if isDispensing {
		mutex.Unlock()
		http.Error(w, "Already dispensing tickets", http.StatusConflict)
		return
	}

	// Mark as dispensing and release the lock
	isDispensing = true
	status = "Starting ticket dispensing..."
	mutex.Unlock()

	// Start dispensing in a goroutine
	go func() {
		dispenseTickets(numTickets)
		mutex.Lock()
		isDispensing = false
		mutex.Unlock()
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": fmt.Sprintf("Dispensing %d tickets...", numTickets),
	})
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	mutex.Lock()
	defer mutex.Unlock()

	// Create response
	response := StatusResponse{
		Status:       status,
		IsDispensing: isDispensing,
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func dispenseTickets(numTickets int) {
	requestedTickets := numTickets

	mutex.Lock()
	status = fmt.Sprintf("Dispensing %d ticket(s)...", requestedTickets)
	mutex.Unlock()

	dispenserPin.Low()
	time.Sleep(100 * time.Millisecond)

	ticketsDispensed := 0
	lastState := sensorPin.Read()

	dispenserPin.High()

	mutex.Lock()
	status = "Dispenser activated"
	mutex.Unlock()

	startTime := time.Now()
	mainTimeout := 60 * time.Second

	ticketTimeout := 3 * time.Second
	lastTicketTime := time.Now()

	for ticketsDispensed < numTickets && time.Since(startTime) < mainTimeout {
		currentState := sensorPin.Read()

		// Detect falling edge (transition from HIGH to LOW)
		// This indicates the sensor has detected a ticket
		if lastState == rpio.Low && currentState == rpio.High {
			ticketsDispensed++

			mutex.Lock()
			status = fmt.Sprintf("Ticket %d/%d dispensed", ticketsDispensed, numTickets)
			mutex.Unlock()

			lastTicketTime = time.Now()
		}

		lastState = currentState
		time.Sleep(5 * time.Millisecond)

		if ticketsDispensed < numTickets &&
			time.Since(lastTicketTime) > ticketTimeout {
			mutex.Lock()
			status = "Warning: No ticket detected for a while. Dispenser may be jammed or out of tickets"
			mutex.Unlock()
			break
		}
	}

	dispenserPin.Low()

	mutex.Lock()
	if ticketsDispensed == numTickets {
		status = fmt.Sprintf("Successfully dispensed %d ticket(s)", requestedTickets)
	} else {
		actualDispensed := ticketsDispensed - 1
		if actualDispensed < 0 {
			actualDispensed = 0
		}
		status = fmt.Sprintf("Dispensing stopped after %d/%d tickets.\nCheck if machine is empty or is not feeding.", actualDispensed, requestedTickets)
		if time.Since(startTime) >= mainTimeout {
			status += ". Operation timed out"
		}
	}
	mutex.Unlock()
}

func createStaticFiles() {
	htmlContent := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
	<meta name="theme-color" content="#021837"/>
	<meta name="apple-mobile-web-app-capable" content="yes">
	<meta name="apple-mobile-web-app-status-bar-style" content="translucent">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Mr. Goose's Honkin' Good Time Ticket Dispenser</title>
    <link rel="stylesheet" href="style.css">
    <link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Bangers&family=Poppins:wght@400;600&display=swap">
</head>
<body>
    <div class="container">
        <header>
            <div class="logo">
                <img src="mghgt.png" alt="Goose icon" class="goose-icon">
            </div>
        </header>

        <div class="card status-card">
            <h2>Ticket Machine Status</h2>
            <div id="status" class="status-display">Initializing...</div>
            <div id="dispensing-indicator" class="indicator">
                <div class="ticket-animation">
                    <div class="ticket"></div>
                    <div class="ticket"></div>
                    <div class="ticket"></div>
                </div>
                <span>Dispensing tickets...</span>
            </div>
        </div>

        <div class="card control-card">
            <h2>Dispense Tickets</h2>
            <div class="ticket-input">
                <div class="number-control">
                    <button id="decreaseBtn" class="round-btn">-</button>
                    <input type="number" id="ticketCount" min="1" value="1">
                    <button id="increaseBtn" class="round-btn">+</button>
                </div>
                <div class="preset-buttons">
                    <button class="preset-btn" data-value="5">5</button>
                    <button class="preset-btn" data-value="10">10</button>
                    <button class="preset-btn" data-value="20">20</button>
                    <button class="preset-btn" data-value="50">50</button>
                </div>
            </div>
            <button id="dispenseBtn" class="primary-btn">
                <span class="btn-icon">🎟️</span> Dispense Tickets
            </button>
        </div>

        <footer>
            <p>Made with <span>❤️</span> in Club 155</p>
        </footer>
    </div>

    <script src="script.js"></script>
</body>
</html>`

	cssContent := `/* Base styles with dark blue theme */
:root {
    --primary: #021837;
    --secondary: #0A2E65;
    --accent: #153A70;
    --highlight: #2A4E80;
    --text: #FFFFFF;
    --text-secondary: #B8C5D9;
    --error: #FF3366;
    --success: #6ECE78;
    --card-bg: #041F45;
}

* {
    box-sizing: border-box;
    margin: 0;
    padding: 0;
}

body {
    font-family: 'Poppins', sans-serif;
    line-height: 1.6;
    background-color: var(--primary);
    color: var(--text);
    min-height: 100vh;
    display: flex;
    justify-content: center;
    align-items: center;
    padding: 20px;
}

.container {
    width: 100%;
    max-width: 500px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: 20px;
}

/* Header styles */
header {
    text-align: center;
}


.logo {
    margin-bottom: 10px;
}

.goose-icon {
    width: auto;
    height: 250px;
    filter: brightness(0) invert(1);
    filter: drop-shadow(2px 2px 3px rgba(0,0,0,0.2));
}

/* Card styles */
.card {
    background-color: var(--card-bg);
    border-radius: 20px;
    padding: 25px;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.3);
    border: 1px solid var(--accent);
}

h2 {
    font-family: 'Bangers', cursive;
    font-size: 1.8rem;
    color: var(--text);
    margin-bottom: 15px;
    text-align: center;
}

/* Status card */
.status-display {
    padding: 15px;
    border-radius: 10px;
    background-color: var(--secondary);
    font-size: 1.1rem;
    text-align: center;
    min-height: 50px;
    display: flex;
    align-items: center;
    justify-content: center;
    margin-bottom: 15px;
    border: 2px dashed var(--accent);
    color: var(--text);
}

.indicator {
    display: none;
    flex-direction: column;
    align-items: center;
    gap: 15px;
    font-weight: bold;
    color: var(--text);
}

.indicator.active {
    display: flex;
}

/* Ticket animation */
.ticket-animation {
    display: flex;
    justify-content: center;
    gap: 15px;
}

.ticket {
    width: 40px;
    height: 20px;
    background-color: var(--highlight);
    border-radius: 5px;
    position: relative;
    animation: ticketFlow 1.2s infinite ease-in-out;
}

.ticket:nth-child(2) {
    animation-delay: 0.4s;
}

.ticket:nth-child(3) {
    animation-delay: 0.8s;
}

@keyframes ticketFlow {
    0% {
        transform: translateY(0);
        opacity: 0;
    }
    50% {
        opacity: 1;
    }
    100% {
        transform: translateY(20px);
        opacity: 0;
    }
}

/* Control card */
.ticket-input {
    margin-bottom: 20px;
}

.number-control {
    display: flex;
    align-items: center;
    justify-content: center;
    margin-bottom: 15px;
}

input[type="number"] {
    width: 100px;
    height: 60px;
    text-align: center;
    font-size: 1.8rem;
    font-weight: bold;
    border: 2px solid var(--accent);
    border-radius: 10px;
    margin: 0 10px;
    -moz-appearance: textfield; /* Firefox */
    padding: 0;
    background-color: var(--secondary);
    color: var(--text);
}

input[type="number"]::-webkit-inner-spin-button,
input[type="number"]::-webkit-outer-spin-button {
    -webkit-appearance: none;
    margin: 0;
}

.round-btn {
    width: 50px;
    height: 50px;
    border-radius: 50%;
    background-color: var(--accent);
    color: var(--text);
    font-size: 1.5rem;
    border: none;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    box-shadow: 0 3px 6px rgba(0,0,0,0.3);
    transition: transform 0.1s, background-color 0.2s;
}

.round-btn:active {
    transform: scale(0.95);
    background-color: var(--highlight);
}

.preset-buttons {
    display: flex;
    justify-content: center;
    gap: 10px;
    margin-bottom: 20px;
}

.preset-btn {
    background-color: var(--secondary);
    border: 2px solid var(--accent);
    color: var(--text);
    border-radius: 10px;
    padding: 8px 0;
    width: 50px;
    font-size: 1.1rem;
    cursor: pointer;
    transition: all 0.2s;
}

.preset-btn:hover, .preset-btn.active {
    background-color: var(--highlight);
}

.primary-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    background-color: var(--accent);
    color: var(--text);
    border: none;
    padding: 15px;
    border-radius: 15px;
    font-family: 'Bangers', cursive;
    font-size: 1.5rem;
    cursor: pointer;
    transition: transform 0.1s, background-color 0.2s;
    box-shadow: 0 4px 8px rgba(0,0,0,0.3);
}

.primary-btn:hover {
    background-color: var(--highlight);
}

.primary-btn:active {
    transform: scale(0.98);
}

.primary-btn:disabled {
    background-color: #2A384F;
    cursor: not-allowed;
}

.btn-icon {
    margin-right: 10px;
    font-size: 1.5rem;
}

/* Footer */
footer {
    text-align: center;
    font-size: 0.9rem;
    color: var(--text-secondary);
    margin-top: 10px;
}

footer span {
    color: var(--error);
}

/* Responsive adjustments */
@media (max-width: 480px) {
    h1 {
        font-size: 2rem;
    }

    .card {
        padding: 20px;
    }

    .number-control {
        flex-wrap: wrap;
    }

    input[type="number"] {
        width: 80px;
        height: 50px;
        font-size: 1.5rem;
    }

    .round-btn {
        width: 45px;
        height: 45px;
    }

    .primary-btn {
        font-size: 1.3rem;
        padding: 12px;
    }
}`

	jsContent := `document.addEventListener('DOMContentLoaded', function() {
    // DOM elements
    const statusElement = document.getElementById('status');
    const dispensingIndicator = document.getElementById('dispensing-indicator');
    const ticketCountInput = document.getElementById('ticketCount');
    const dispenseBtn = document.getElementById('dispenseBtn');
    const decreaseBtn = document.getElementById('decreaseBtn');
    const increaseBtn = document.getElementById('increaseBtn');
    const presetButtons = document.querySelectorAll('.preset-btn');

    // Number input controls
    function updateTicketCount(value) {
        let count = parseInt(ticketCountInput.value) || 1;
        count += value;

        // Ensure minimum value of 1
        count = Math.max(1, count);

        ticketCountInput.value = count;
    }

    decreaseBtn.addEventListener('click', function() {
        updateTicketCount(-1);
    });

    increaseBtn.addEventListener('click', function() {
        updateTicketCount(1);
    });

    // Handle preset buttons
    presetButtons.forEach(button => {
        button.addEventListener('click', function() {
            const value = parseInt(this.dataset.value);
            ticketCountInput.value = value;

            // Visual feedback - highlight selected preset
            presetButtons.forEach(btn => btn.classList.remove('active'));
            this.classList.add('active');
        });
    });

    // Ensure input is valid on manual change
    ticketCountInput.addEventListener('change', function() {
        let value = parseInt(this.value) || 1;
        value = Math.max(1, value);
        this.value = value;

        // Reset preset button highlights
        presetButtons.forEach(btn => btn.classList.remove('active'));
    });

    // Set up polling for status updates
    function updateStatus() {
        fetch('/api/status')
            .then(response => response.json())
            .then(data => {
                statusElement.textContent = data.status;

                // Update dispensing indicator
                if (data.isDispensing) {
                    dispensingIndicator.classList.add('active');
                    dispenseBtn.disabled = true;
                } else {
                    dispensingIndicator.classList.remove('active');
                    dispenseBtn.disabled = false;
                }
            })
            .catch(error => {
                console.error('Error fetching status:', error);
                statusElement.textContent = 'Error connecting to server';
            });
    }

    // Poll status every second
    updateStatus();
    setInterval(updateStatus, 1000);

    // Handle dispense button click
    dispenseBtn.addEventListener('click', function() {
        const ticketCount = ticketCountInput.value;

        if (ticketCount < 1) {
            alert('Please enter a valid number of tickets');
            return;
        }

        // Disable button to prevent multiple clicks
        dispenseBtn.disabled = true;

        // Add active visual feedback
        dispenseBtn.style.backgroundColor = '#2A4E80';
        setTimeout(() => {
            dispenseBtn.style.backgroundColor = '';
        }, 300);

        // Send dispense request
        const formData = new FormData();
        formData.append('tickets', ticketCount);

        fetch('/api/dispense', {
            method: 'POST',
            body: formData
        })
        .then(response => {
            if (!response.ok) {
                return response.text().then(text => {
                    throw new Error(text);
                });
            }
            return response.json();
        })
        .then(data => {
            console.log('Success:', data);
            // Status updates will be handled by the polling function
        })
        .catch(error => {
            console.error('Error:', error);
            statusElement.textContent = 'Error: ' + error.message;
            dispenseBtn.disabled = false;
        });
    });

    // Add touch-friendly features for mobile
    document.querySelectorAll('button').forEach(button => {
        // Remove outline on touch
        button.addEventListener('touchstart', function() {
            this.style.outline = 'none';
        });

        // Add active state for touch feedback
        button.addEventListener('touchstart', function() {
            this.classList.add('touching');
        });

        button.addEventListener('touchend', function() {
            this.classList.remove('touching');
        });
    });
});`

	// Write files
	os.WriteFile("./static/index.html", []byte(htmlContent), 0644)
	os.WriteFile("./static/style.css", []byte(cssContent), 0644)
	os.WriteFile("./static/script.js", []byte(jsContent), 0644)
}
