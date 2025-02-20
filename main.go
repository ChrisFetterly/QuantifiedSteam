// main.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
	"tinygo.org/x/bluetooth"
)

// ---------------------------
// Data Structures & Monitor
// ---------------------------

// WSMessage is the JSON message structure.
type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// SensorReading contains sensor values.
type SensorReading struct {
	Temperature   float64 `json:"temperature"`
	Humidity      float64 `json:"humidity"`
	HeartRate     *int    `json:"heartRate"`
	HrConnected   bool    `json:"hrConnected"`
	HeaterSetting int     `json:"heaterSetting"`
	LocationState string  `json:"locationState"`
	DoorState     string  `json:"doorState"`
	PositionState string  `json:"positionState"`
}

// HRDevice represents a discovered heart rate device.
type HRDevice struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Monitor holds the sensor state, BLE adapter, DB connection, etc.
type Monitor struct {
	sensor *SHT31D

	bleAdapter *bluetooth.Adapter
	hrDevice   bluetooth.Device
	hrChar     bluetooth.DeviceCharacteristic
	currentHR  *int
	hrConnected bool
	connectedDeviceID *int

	mu               sync.Mutex
	availableDevices []bluetooth.Address
	deviceList       []HRDevice
	scanning         bool
	heaterSetting    int
	LocationState    string // "inside" or "outside"
	DoorState        string // "open" or "closed"
	PositionState    string // "standing" or "sitting"

	currentSessionID *int64
	sessionStart     time.Time
	db               *sql.DB
	running          bool
}

// Close shuts down the BLE device and the database.
func (m *Monitor) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hrConnected {
		m.hrDevice.Disconnect()
	}
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}

// initializeDatabase opens (or creates) the SQLite database and creates the tables.
func initializeDatabase(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("error opening database: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(time.Hour)
	_, err = db.Exec("PRAGMA journal_mode=WAL;")
	if err != nil {
		return nil, fmt.Errorf("failed to enable WAL mode: %v", err)
	}
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS sessions (
            id INTEGER PRIMARY KEY,
            start_time DATETIME DEFAULT CURRENT_TIMESTAMP,
            end_time DATETIME,
            duration_seconds INTEGER,
            max_temperature REAL,
            max_heart_rate INTEGER,
            max_humidity REAL,
            notes TEXT
        );
        CREATE TABLE IF NOT EXISTS measurements (
            id INTEGER PRIMARY KEY,
            session_id INTEGER,
            timestamp DATETIME NOT NULL,
            temperature REAL,
            humidity REAL,
            heart_rate INTEGER,
            heater_setting INTEGER,
            FOREIGN KEY (session_id) REFERENCES sessions(id)
        );
        CREATE TABLE IF NOT EXISTS state_changes (
            id INTEGER PRIMARY KEY,
            session_id INTEGER,
            timestamp DATETIME NOT NULL,
            state_type TEXT NOT NULL,
            new_state TEXT NOT NULL,
            FOREIGN KEY (session_id) REFERENCES sessions(id)
        );
    `)
	if err != nil {
		return nil, fmt.Errorf("error creating tables: %v", err)
	}
	return db, nil
}

// setHeaterSetting sets the heater value (0–16).
func (m *Monitor) setHeaterSetting(setting int) error {
	if setting < 0 || setting > 16 {
		return fmt.Errorf("heater setting must be between 0 and 16")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heaterSetting = setting
	return nil
}

// NewMonitor creates and returns a new Monitor.
func NewMonitor() (*Monitor, error) {
	sensor, err := NewSHT31D()
	if err != nil {
		return nil, err
	}
	adapter := bluetooth.DefaultAdapter
	if err = adapter.Enable(); err != nil {
		log.Printf("BLE adapter enable failed: %v", err)
		return nil, fmt.Errorf("bluetooth adapter enable failed: %v", err)
	}
	db, err := initializeDatabase("sauna_data.db")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %v", err)
	}
	return &Monitor{
		sensor:        sensor,
		bleAdapter:    adapter,
		LocationState: "outside",
		DoorState:     "closed",
		PositionState: "standing",
		db:            db,
	}, nil
}

// startScan scans for heart rate devices for 10 seconds.
// Devices are deduplicated based on their address to prevent repetition.
// startScan scans for heart rate devices for 10 seconds.
// Modified so that if a device is already connected, the device list is not cleared.
// Devices are deduplicated based on their address to prevent repetition, and connected devices are not re-added.
func (m *Monitor) startScan() {
    m.mu.Lock()
    if m.scanning {
        m.mu.Unlock()
        return
    }
    m.scanning = true
    // Only clear the device list if no HR device is connected.
    if !m.hrConnected {
        m.availableDevices = nil
        m.deviceList = nil
    }
    seen := make(map[string]bool) // Track seen devices by address
    m.mu.Unlock()

    done := make(chan bool)
    go func() {
        time.Sleep(10 * time.Second)
        m.bleAdapter.StopScan()
        m.mu.Lock()
        m.scanning = false
        m.mu.Unlock()
        done <- true
    }()

    go func() {
        err := m.bleAdapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
            name := result.LocalName()
            addr := result.Address.String()
            if name != "" && result.HasServiceUUID(bluetooth.New16BitUUID(0x180D)) {
                m.mu.Lock()
                defer m.mu.Unlock()
                // Check if this device is already connected
                isConnected := false
                for i, deviceAddr := range m.availableDevices {
                    if deviceAddr.String() == addr && m.deviceList[i].Status == "connected" {
                        isConnected = true
                        break
                    }
                }
                if !seen[addr] && !isConnected { // Only add if not seen before and not already connected
                    seen[addr] = true
                    id := len(m.availableDevices)
                    m.availableDevices = append(m.availableDevices, result.Address)
                    m.deviceList = append(m.deviceList, HRDevice{
                        ID:     id,
                        Name:   name,
                        Status: "available",
                    })
                    log.Printf("Found unique device: %s (%s)", name, addr)
                }
            }
        })
        if err != nil {
            log.Printf("Scan error: %v", err)
        }
    }()
    <-done
    log.Printf("Scan completed. Found %d unique devices.", len(m.availableDevices))
}



// connectToDevice attempts to connect to a device (by its index).
// We stop scanning and wait 2 seconds before connecting.
// The connection timeout is increased to 45 seconds.
// In connectToDevice (lines 206–236, updated)
func (m *Monitor) connectToDevice(deviceID int) error {
    m.mu.Lock()
    if deviceID >= len(m.deviceList) {
        m.mu.Unlock()
        return fmt.Errorf("invalid device ID: %d", deviceID)
    }
    status := m.deviceList[deviceID].Status
    if status == "connecting" || status == "connected" {
        m.mu.Unlock()
        return nil
    }
    m.deviceList[deviceID].Status = "connecting"
    addr := m.availableDevices[deviceID]
    m.mu.Unlock()

    // Stop scanning to ensure the adapter is free
    log.Printf("Stopping scan before connecting to device %s...", addr.String())
    m.bleAdapter.StopScan()
    time.Sleep(2 * time.Second) // Increased delay

    done := make(chan error)
    go func() {
        log.Printf("Attempting to connect to device %s...", addr.String())
        // Convert 45 seconds to milliseconds (45 * 1000)
        device, err := m.bleAdapter.Connect(addr, bluetooth.ConnectionParams{
            Timeout: bluetooth.Duration(45000), // 45 seconds = 45,000 milliseconds
        })
        if err != nil {
            log.Printf("Connection failed for device %s: %v", addr.String(), err)
            done <- fmt.Errorf("connection failed: %v", err)
            return
        }
        m.hrDevice = device
        err = m.connectAndSetupHR(device)
        if err != nil {
            log.Printf("HR setup failed for device %s: %v", addr.String(), err)
            device.Disconnect() // Clean up on failure
        }
        done <- err
    }()
    select {
    case err := <-done:
        m.mu.Lock()
        if err != nil {
            m.deviceList[deviceID].Status = "failed"
            m.mu.Unlock()
            return err
        }
        m.deviceList[deviceID].Status = "connected"
        m.connectedDeviceID = &deviceID
        m.mu.Unlock()
        log.Printf("Successfully connected to device %s", addr.String())
        return nil
    case <-time.After(45 * time.Second):
        m.mu.Lock()
        m.deviceList[deviceID].Status = "failed"
        m.mu.Unlock()
        log.Printf("Connection timeout for device %s", addr.String())
        return fmt.Errorf("connection timeout for device %s", addr.String())
    }
}

// connectAndSetupHR discovers the HR service and enables notifications.
// We try up to 5 times with 2-second delays.
func (m *Monitor) connectAndSetupHR(device bluetooth.Device) error {
    for i := 0; i < 5; i++ {
        log.Printf("HR service discovery attempt %d for device %s", i+1, device.Address.String())
        services, err := device.DiscoverServices([]bluetooth.UUID{bluetooth.New16BitUUID(0x180D)})
        if err != nil {
            log.Printf("Service discovery error on attempt %d: %v", i+1, err)
            time.Sleep(2 * time.Second)
            continue
        }
        for _, service := range services {
            if service.UUID().String() == "0000180d-0000-1000-8000-00805f9b34fb" { // Heart Rate Service UUID
                chars, err := service.DiscoverCharacteristics([]bluetooth.UUID{bluetooth.New16BitUUID(0x2A37)})
                if err != nil {
                    log.Printf("Characteristic discovery error: %v", err)
                    continue
                }
                for _, char := range chars {
                    if char.UUID().String() == "00002a37-0000-1000-8000-00805f9b34fb" { // Heart Rate Measurement UUID
                        m.mu.Lock()
                        m.hrChar = char
                        m.hrConnected = true
                        m.mu.Unlock()
                        return char.EnableNotifications(func(buf []byte) {
                            if len(buf) >= 2 {
                                hr := int(buf[1]) // Assuming byte 1 contains the heart rate value
                                m.mu.Lock()
                                m.currentHR = &hr
                                m.mu.Unlock()
                            }
                        })
                    }
                }
            }
        }
    }
    return fmt.Errorf("heart rate service not found on device %s", device.Address.String())
}

func (m *Monitor) getDeviceList() []HRDevice {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deviceList
}

func (m *Monitor) getCurrentReading() SensorReading {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.sensor.Read(); err != nil {
		log.Printf("Sensor read error: %v", err)
	}
	return SensorReading{
		Temperature:   m.sensor.Temperature(),
		Humidity:      m.sensor.Humidity(),
		HeartRate:     m.currentHR,
		HrConnected:   m.hrConnected,
		HeaterSetting: m.heaterSetting,
		LocationState: m.LocationState,
		DoorState:     m.DoorState,
		PositionState: m.PositionState,
	}
}

func (m *Monitor) startSession() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return fmt.Errorf("session already in progress")
	}
	result, err := m.db.Exec(`INSERT INTO sessions (start_time) VALUES (datetime('now'))`)
	if err != nil {
		return fmt.Errorf("failed to start session: %v", err)
	}
	sid, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get session id: %v", err)
	}
	m.currentSessionID = &sid
	m.sessionStart = time.Now()
	m.running = true
	return nil
}

func (m *Monitor) stopSession() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running || m.currentSessionID == nil {
		return fmt.Errorf("no session in progress")
	}
	dur := time.Since(m.sessionStart)
	_, err := m.db.Exec(`UPDATE sessions SET end_time = datetime('now'), duration_seconds = ? WHERE id = ?`,
		int(dur.Seconds()), *m.currentSessionID)
	if err != nil {
		return fmt.Errorf("failed to stop session: %v", err)
	}
	m.running = false
	m.currentSessionID = nil
	return nil
}

func (m *Monitor) logStateChange(stateType, newState string) error {
	m.mu.Lock()
	sid := m.currentSessionID
	m.mu.Unlock()
	if sid == nil {
		defaultID := int64(0)
		sid = &defaultID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO state_changes (
			session_id,
			timestamp,
			state_type,
			new_state
		) VALUES (?, strftime('%Y-%m-%d %H:%M:%f', 'now'), ?, ?)
	`, *sid, stateType, newState)
	if err != nil {
		return fmt.Errorf("failed to log state change: %v", err)
	}
	return nil
}

func (m *Monitor) toggleLocation() error {
	m.mu.Lock()
	if m.LocationState == "outside" {
		m.LocationState = "inside"
	} else {
		m.LocationState = "outside"
	}
	state := m.LocationState
	m.mu.Unlock()
	go m.logStateChange("location", state)
	return nil
}

func (m *Monitor) toggleDoor() error {
	m.mu.Lock()
	if m.DoorState == "closed" {
		m.DoorState = "open"
	} else {
		m.DoorState = "closed"
	}
	state := m.DoorState
	m.mu.Unlock()
	go m.logStateChange("door", state)
	return nil
}

func (m *Monitor) togglePosition() error {
	m.mu.Lock()
	if m.PositionState == "standing" {
		m.PositionState = "sitting"
	} else {
		m.PositionState = "standing"
	}
	state := m.PositionState
	m.mu.Unlock()
	go m.logStateChange("position", state)
	return nil
}

func (m *Monitor) logMeasurement() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running || m.currentSessionID == nil {
		return fmt.Errorf("no active session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO measurements (
			session_id,
			timestamp,
			temperature,
			humidity,
			heart_rate,
			heater_setting
		) VALUES (?, datetime('now'), ?, ?, ?, ?)
	`, *m.currentSessionID, m.sensor.Temperature(), m.sensor.Humidity(), m.currentHR, m.heaterSetting)
	if err != nil {
		return fmt.Errorf("failed to log measurement: %v", err)
	}
	return nil
}

// ---------------------------
// WebSocket Handler (Real-Time & Data Queries)
// ---------------------------

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

const (
	wsPongWait   = 60 * time.Second
	wsPingPeriod = (wsPongWait * 9) / 10
)

func wsHandler(monitor *Monitor, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	sendChan := make(chan WSMessage, 50)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	var closeOnce sync.Once

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case msg := <-sendChan:
				if err := conn.WriteJSON(msg); err != nil {
					log.Printf("Write error: %v", err)
					closeOnce.Do(func() {
						close(stop)
					})
					return
				}
			case <-stop:
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(wsPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Printf("Ping error: %v", err)
					closeOnce.Do(func() {
						close(stop)
					})
					return
				}
			case <-stop:
				return
			}
		}
	}()

	sendChan <- WSMessage{
		Type:    "reading",
		Payload: monitor.getCurrentReading(),
	}
	if monitor.running {
		sendChan <- WSMessage{
			Type:    "sessionStatus",
			Payload: map[string]interface{}{"status": "started"},
		}
	} else {
		sendChan <- WSMessage{
			Type:    "sessionStatus",
			Payload: map[string]interface{}{"status": "stopped"},
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				log.Printf("Read error: %v", err)
				closeOnce.Do(func() {
					close(stop)
				})
				return
			}
			switch msg.Type {
			case "scan":
				go func() {
					monitor.startScan()
					sendChan <- WSMessage{
						Type:    "devices",
						Payload: monitor.getDeviceList(),
					}
				}()
			case "toggleLocation":
				go func() {
					if err := monitor.toggleLocation(); err != nil {
						sendChan <- WSMessage{Type: "error", Payload: err.Error()}
					} else {
						sendChan <- WSMessage{
							Type: "stateChange",
							Payload: map[string]interface{}{
								"type":  "location",
								"state": monitor.LocationState,
							},
						}
					}
				}()
			case "toggleDoor":
				go func() {
					if err := monitor.toggleDoor(); err != nil {
						sendChan <- WSMessage{Type: "error", Payload: err.Error()}
					} else {
						sendChan <- WSMessage{
							Type: "stateChange",
							Payload: map[string]interface{}{
								"type":  "door",
								"state": monitor.DoorState,
							},
						}
					}
				}()
			case "togglePosition":
				go func() {
					if err := monitor.togglePosition(); err != nil {
						sendChan <- WSMessage{Type: "error", Payload: err.Error()}
					} else {
						sendChan <- WSMessage{
							Type: "stateChange",
							Payload: map[string]interface{}{
								"type":  "position",
								"state": monitor.PositionState,
							},
						}
					}
				}()
			case "start":
				go func() {
					if err := monitor.startSession(); err != nil {
						sendChan <- WSMessage{Type: "error", Payload: err.Error()}
					} else {
						sendChan <- WSMessage{
							Type: "sessionStatus",
							Payload: map[string]interface{}{"status": "started"},
						}
					}
				}()
			case "stop":
				go func() {
					if err := monitor.stopSession(); err != nil {
						sendChan <- WSMessage{Type: "error", Payload: err.Error()}
					} else {
						sendChan <- WSMessage{
							Type: "sessionStatus",
							Payload: map[string]interface{}{"status": "stopped"},
						}
					}
				}()
			case "setHeater":
				if payload, ok := msg.Payload.(map[string]interface{}); ok {
					if setting, ok := payload["setting"].(float64); ok {
						go func() {
							if err := monitor.setHeaterSetting(int(setting)); err != nil {
								sendChan <- WSMessage{Type: "error", Payload: err.Error()}
								return
							}
							if monitor.running {
								if err := monitor.logStateChange("heater", fmt.Sprintf("%d", monitor.heaterSetting)); err != nil {
									log.Printf("Failed to log heater change: %v", err)
								}
							}
							sendChan <- WSMessage{
								Type: "stateChange",
								Payload: map[string]interface{}{
									"type":  "heater",
									"state": monitor.heaterSetting,
								},
							}
						}()
					}
				}
			case "connect":
				var deviceID int
				if payload, ok := msg.Payload.(map[string]interface{}); ok {
					if idFloat, ok := payload["deviceId"].(float64); ok {
						deviceID = int(idFloat)
						monitor.mu.Lock()
						var status string
						if deviceID < len(monitor.deviceList) {
							status = monitor.deviceList[deviceID].Status
						}
						monitor.mu.Unlock()
						if status == "connecting" || status == "connected" {
							break
						}
						go func() {
							if err := monitor.connectToDevice(deviceID); err != nil {
								sendChan <- WSMessage{Type: "error", Payload: err.Error()}
							} else {
								sendChan <- WSMessage{
									Type: "deviceStatus",
									Payload: map[string]interface{}{
										"id":     deviceID,
										"status": "connected",
									},
								}
							}
						}()
					}
				}
			case "getSessions":
				go func() {
					rows, err := monitor.db.Query("SELECT id, start_time, end_time, duration_seconds, max_temperature, max_heart_rate, max_humidity, notes FROM sessions ORDER BY id DESC")
					if err != nil {
						sendChan <- WSMessage{Type: "error", Payload: err.Error()}
						return
					}
					defer rows.Close()
					var sessions []map[string]interface{}
					for rows.Next() {
						var id int
						var startTime, endTime sql.NullString
						var duration sql.NullInt64
						var maxTemp, maxHumidity sql.NullFloat64
						var maxHR sql.NullInt64
						var notes sql.NullString
						if err := rows.Scan(&id, &startTime, &endTime, &duration, &maxTemp, &maxHR, &maxHumidity, &notes); err != nil {
							sendChan <- WSMessage{Type: "error", Payload: err.Error()}
							return
						}
						session := map[string]interface{}{
							"id":               id,
							"start_time":       startTime.String,
							"end_time":         endTime.String,
							"duration_seconds": duration.Int64,
							"max_temperature":  maxTemp.Float64,
							"max_heart_rate":   maxHR.Int64,
							"max_humidity":     maxHumidity.Float64,
							"notes":            notes.String,
						}
						sessions = append(sessions, session)
					}
					sendChan <- WSMessage{Type: "sessions", Payload: sessions}
				}()
			case "getMeasurements":
				go func() {
					sessionId, ok := msg.Payload.(map[string]interface{})["sessionId"].(float64)
					if !ok {
						sendChan <- WSMessage{Type: "error", Payload: "sessionId required"}
						return
					}
					rows, err := monitor.db.Query("SELECT id, timestamp, temperature, humidity, heart_rate, heater_setting FROM measurements WHERE session_id = ? ORDER BY timestamp ASC", int(sessionId))
					if err != nil {
						sendChan <- WSMessage{Type: "error", Payload: err.Error()}
						return
					}
					defer rows.Close()
					var measurements []map[string]interface{}
					for rows.Next() {
						var id int
						var timestamp string
						var temp, humidity float64
						var heartRate sql.NullInt64
						var heaterSetting int
						if err := rows.Scan(&id, &timestamp, &temp, &humidity, &heartRate, &heaterSetting); err != nil {
							sendChan <- WSMessage{Type: "error", Payload: err.Error()}
							return
						}
						m := map[string]interface{}{
							"id":             id,
							"timestamp":      timestamp,
							"temperature":    temp,
							"humidity":       humidity,
							"heart_rate":     heartRate.Int64,
							"heater_setting": heaterSetting,
						}
						measurements = append(measurements, m)
					}
					sendChan <- WSMessage{Type: "measurements", Payload: measurements}
				}()
			case "getStateChanges":
				go func() {
					sessionId, ok := msg.Payload.(map[string]interface{})["sessionId"].(float64)
					if !ok {
						sendChan <- WSMessage{Type: "error", Payload: "sessionId required"}
						return
					}
					rows, err := monitor.db.Query("SELECT id, timestamp, state_type, new_state FROM state_changes WHERE session_id = ? ORDER BY timestamp ASC", int(sessionId))
					if err != nil {
						sendChan <- WSMessage{Type: "error", Payload: err.Error()}
						return
					}
					defer rows.Close()
					var changes []map[string]interface{}
					for rows.Next() {
						var id int
						var timestamp, stateType, newState string
						if err := rows.Scan(&id, &timestamp, &stateType, &newState); err != nil {
							sendChan <- WSMessage{Type: "error", Payload: err.Error()}
							return
						}
						change := map[string]interface{}{
							"id":         id,
							"timestamp":  timestamp,
							"state_type": stateType,
							"new_state":  newState,
						}
						changes = append(changes, change)
					}
					sendChan <- WSMessage{Type: "stateChanges", Payload: changes}
				}()
			}
		}
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			reading := monitor.getCurrentReading()
			if monitor.running {
				go func() {
					if err := monitor.logMeasurement(); err != nil {
						log.Printf("Measurement logging error: %v", err)
					}
				}()
			}
			sendChan <- WSMessage{Type: "reading", Payload: reading}
		}
	}()

	wg.Wait()
}

func main() {
	log.Println("Starting server...")
	monitor, err := NewMonitor()
	if err != nil {
		log.Fatal("Failed to initialize monitor:", err)
	}
	defer monitor.Close()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		wsHandler(monitor, w, r)
	})

	fs := http.FileServer(http.Dir("./public"))
	http.Handle("/", fs)

	log.Println("Listening on :5000")
	if err := http.ListenAndServe(":5000", nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}
