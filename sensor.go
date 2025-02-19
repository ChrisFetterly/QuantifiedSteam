package main

import (
    "encoding/binary"
    "fmt"
    "time"

    "periph.io/x/conn/v3/i2c"
    "periph.io/x/conn/v3/i2c/i2creg"
    "periph.io/x/host/v3"
)

type SHT31D struct {
    dev  i2c.Dev
    bus  i2c.BusCloser
    temp float64
    hum  float64
}

func NewSHT31D() (*SHT31D, error) {
    // Initialize host
    if _, err := host.Init(); err != nil {
        return nil, fmt.Errorf("failed to initialize periph: %v", err)
    }

    // Open default I2C bus
    bus, err := i2creg.Open("/dev/i2c-1")
    if err != nil {
        return nil, fmt.Errorf("failed to open I2C: %v", err)
    }

    // Create device
    dev := i2c.Dev{Bus: bus, Addr: 0x44}

    return &SHT31D{
        dev: dev,
        bus: bus,
    }, nil
}

func (s *SHT31D) Read() error {
    // Send high repeatability measurement command
    if err := s.dev.Tx([]byte{0x2C, 0x06}, nil); err != nil {
        return fmt.Errorf("failed to send command: %v", err)
    }

    // Wait for measurement
    time.Sleep(15 * time.Millisecond)

    // Read data
    data := make([]byte, 6)
    if err := s.dev.Tx(nil, data); err != nil {
        return fmt.Errorf("failed to read data: %v", err)
    }

    // Convert temperature
    tempRaw := binary.BigEndian.Uint16(data[0:2])
    s.temp = float64(tempRaw)*175.0/65535.0 - 45.0

    // Convert humidity
    humRaw := binary.BigEndian.Uint16(data[3:5])
    s.hum = float64(humRaw) * 100.0 / 65535.0

    return nil
}

func (s *SHT31D) Temperature() float64 {
    return s.temp
}

func (s *SHT31D) Humidity() float64 {
    return s.hum
}

func (s *SHT31D) Close() error {
    return s.bus.Close()
}
