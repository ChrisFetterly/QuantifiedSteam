# Sauna Monitor

A real-time sauna monitoring system built with Go that tracks temperature, humidity, heart rate, and session data.

## Features

- Real-time temperature and humidity monitoring using SHT31D sensor
- Heart rate monitoring via Bluetooth LE devices
- Session tracking and data logging
- Interactive web interface for monitoring and control
- Data explorer for historical session analysis
- SQLite database for persistent storage

## Requirements

- Go 1.x
- SQLite3
- Web browser with WebSocket support
- Bluetooth LE capable device (for heart rate monitoring)
- SHT31D temperature/humidity sensor

## Setup

1. Clone the repository
2. Ensure you have Go installed
3. Install dependencies:
   ```bash
   go mod init sauna-go
   go mod tidy
   ```

## Usage

1. Start the server:
   ```bash
   go run main.go
   ```

2. Access the web interface:
   - Main interface: `http://localhost:5000`
   - Data explorer: `http://localhost:5000/explore.html`

## Project Structure

- `main.go` - Main server implementation
- `sensor.go` - SHT31D sensor interface
- `public/` - Web interface files
  - `index.html` - Main monitoring interface
  - `explore.html` - Data explorer interface
  - `app.js` - Main interface JavaScript
  - `explore.js` - Data explorer JavaScript
  - `style.css` - Main interface styles
  - `explore.css` - Data explorer styles

## Features

### Monitoring
- Real-time temperature and humidity readings
- Heart rate monitoring with BLE device support
- Heater control (0-16 settings)
- Location state tracking (inside/outside)
- Door state tracking (open/closed)
- Position state tracking (standing/sitting)

### Data Management
- Session recording with start/stop functionality
- Automatic measurement logging
- State change tracking
- Historical data viewing and analysis

## License

MIT License
