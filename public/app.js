(function () {
  let ws;
  let reconnectInterval = 3000; // 3 seconds between reconnection attempts

  function connectWebSocket() {
    ws = new WebSocket("ws://" + window.location.host + "/ws");

    ws.onopen = () => {
      console.log("WebSocket connected");
      updateStatus("Connected");
    };

    ws.onclose = (event) => {
      console.log("WebSocket closed:", event);
      updateStatus("Disconnected");
      // Attempt reconnection after a delay
      setTimeout(connectWebSocket, reconnectInterval);
    };

    ws.onerror = (err) => {
      console.error("WebSocket error:", err);
      updateStatus("Error");
    };

    ws.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      console.log("Received:", msg);
      switch (msg.type) {
        case "reading":
          updateSensorData(msg.payload);
          break;
        case "stateChange":
          updateState(msg.payload);
          break;
        case "sessionStatus":
          updateSessionStatus(msg.payload);
          break;
        case "devices":
          updateDevices(msg.payload);
          break;
        case "deviceStatus":
          // refresh device list on status change.
          ws.send(JSON.stringify({ type: "scan" }));
          break;
        case "error":
          alert("Error: " + msg.payload);
          break;
        default:
          console.log("Unhandled message:", msg);
      }
    };
  }

  // Initialize the connection when the page loads.
  connectWebSocket();

  function updateStatus(status) {
    document.getElementById("wsStatus").textContent = status;
  }

  function updateSensorData(data) {
    document.getElementById("temperature").textContent = data.temperature.toFixed(1) + " °C";
    document.getElementById("humidity").textContent = data.humidity.toFixed(1) + " %";
    document.getElementById("heartRate").textContent =
      data.hrConnected ? (data.heartRate ? data.heartRate : "Waiting...") : "Not Connected";
    document.getElementById("heaterSetting").textContent = data.heaterSetting;
  }

  function updateState(payload) {
    const type = payload.type;
    const state = payload.state;
    if (type === "location") {
      document.getElementById("toggleLocation").textContent =
        state === "outside" ? "Enter Sauna" : "Exit Sauna";
    } else if (type === "door") {
      document.getElementById("toggleDoor").textContent =
        state === "closed" ? "Open Doors" : "Close Doors";
    } else if (type === "position") {
      document.getElementById("togglePosition").textContent =
        state === "standing" ? "Sitting" : "Standing";
    } else if (type === "heater") {
      document.getElementById("heaterSetting").textContent = state;
    }
  }

  function updateSessionStatus(payload) {
    if (payload.status === "started") {
      document.getElementById("startSession").disabled = true;
      document.getElementById("stopSession").disabled = false;
    } else if (payload.status === "stopped") {
      document.getElementById("startSession").disabled = false;
      document.getElementById("stopSession").disabled = true;
    }
  }

  function updateDevices(devices) {
    const container = document.getElementById("deviceList");
    container.innerHTML = "";
    devices.forEach((device) => {
      const div = document.createElement("div");
      div.className = "device-item";
      const nameSpan = document.createElement("span");
      nameSpan.textContent = device.name;
      const btn = document.createElement("button");
      
      if (device.status === "connecting") {
        btn.textContent = "Connecting…";
        btn.disabled = true;
      } else if (device.status === "connected") {
        btn.textContent = "Connected";
        btn.disabled = true;
      } else {
        btn.textContent = "Connect";
        btn.onclick = function(e) {
          e.target.textContent = "Connecting…";
          e.target.disabled = true;
          ws.send(JSON.stringify({ type: "connect", payload: { deviceId: device.id } }));
        };
      }
      
      div.appendChild(nameSpan);
      div.appendChild(btn);
      container.appendChild(div);
    });
  }

  // Attach event listeners to control elements.
  document.getElementById("toggleLocation").addEventListener("click", () => {
    ws.send(JSON.stringify({ type: "toggleLocation" }));
  });

  document.getElementById("toggleDoor").addEventListener("click", () => {
    ws.send(JSON.stringify({ type: "toggleDoor" }));
  });

  document.getElementById("togglePosition").addEventListener("click", () => {
    ws.send(JSON.stringify({ type: "togglePosition" }));
  });

  document.getElementById("setHeater").addEventListener("click", () => {
    const val = parseInt(document.getElementById("heaterInput").value);
    ws.send(JSON.stringify({ type: "setHeater", payload: { setting: val } }));
  });

  document.getElementById("startSession").addEventListener("click", () => {
    ws.send(JSON.stringify({ type: "start" }));
  });

  document.getElementById("stopSession").addEventListener("click", () => {
    ws.send(JSON.stringify({ type: "stop" }));
  });

  document.getElementById("scanDevices").addEventListener("click", () => {
    ws.send(JSON.stringify({ type: "scan" }));
  });
})();
