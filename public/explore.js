(function () {
  const ws = new WebSocket("ws://" + window.location.host + "/ws");
  let sessions = [];
  let currentSession = null;
  let measurementChart = null;

  ws.onopen = () => {
    console.log("WebSocket connected for data explorer");
    loadSessions();
  };

  ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    console.log("Explorer received:", msg);
    switch (msg.type) {
      case "sessions":
        sessions = msg.payload;
        renderSessions();
        break;
      case "measurements":
        renderMeasurements(msg.payload);
        break;
      case "stateChanges":
        renderStateChanges(msg.payload);
        break;
      case "error":
        console.error("Error:", msg.payload);
        break;
      default:
        console.log("Unhandled message:", msg);
    }
  };

  function loadSessions() {
    ws.send(JSON.stringify({ type: "getSessions" }));
  }

  function renderSessions() {
    const tbody = document.querySelector("#sessionsTable tbody");
    tbody.innerHTML = "";
    sessions.forEach((session) => {
      const tr = document.createElement("tr");
      tr.innerHTML = `
        <td>${session.id}</td>
        <td>${session.start_time}</td>
        <td>${session.end_time}</td>
        <td>${session.duration_seconds}</td>
      `;
      tr.style.cursor = "pointer";
      tr.onclick = () => {
        currentSession = session.id;
        loadSessionDetails(session.id);
      };
      tbody.appendChild(tr);
    });
  }

  function loadSessionDetails(sessionId) {
    ws.send(JSON.stringify({ type: "getMeasurements", payload: { sessionId } }));
    ws.send(JSON.stringify({ type: "getStateChanges", payload: { sessionId } }));
    document.getElementById("sessionList").style.display = "none";
    document.getElementById("sessionDetails").style.display = "block";
  }

  function renderMeasurements(measurements) {
    const labels = measurements.map((m) => m.timestamp);
    const temps = measurements.map((m) => m.temperature);
    const hums = measurements.map((m) => m.humidity);
    const ctx = document.getElementById("measurementChart").getContext("2d");
    if (measurementChart) {
      measurementChart.destroy();
    }
    measurementChart = new Chart(ctx, {
      type: "line",
      data: {
        labels: labels,
        datasets: [
          {
            label: "Temperature (°C)",
            data: temps,
            borderColor: "#ff5722",
            backgroundColor: "rgba(255, 87, 34, 0.2)",
            fill: false,
            tension: 0.1
          },
          {
            label: "Humidity (%)",
            data: hums,
            borderColor: "#03a9f4",
            backgroundColor: "rgba(3, 169, 244, 0.2)",
            fill: false,
            tension: 0.1
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        scales: {
          x: {
            ticks: {
              color: "#00ffea",
              font: { family: "'Share Tech Mono'" }
            },
            grid: {
              color: "rgba(0, 255, 234, 0.2)"
            }
          },
          y: {
            ticks: {
              color: "#00ffea",
              font: { family: "'Share Tech Mono'" }
            },
            grid: {
              color: "rgba(0, 255, 234, 0.2)"
            }
          }
        },
        plugins: {
          legend: {
            labels: {
              color: "#00ffea",
              font: { family: "'Share Tech Mono'" }
            }
          }
        }
      },
    });
  }

  function renderStateChanges(stateChanges) {
    const tbody = document.querySelector("#stateChangesTable tbody");
    tbody.innerHTML = "";
    stateChanges.forEach((sc) => {
      const tr = document.createElement("tr");
      tr.innerHTML = `
        <td>${sc.timestamp}</td>
        <td>${sc.state_type}</td>
        <td>${sc.new_state}</td>
      `;
      tbody.appendChild(tr);
    });
  }

  document.getElementById("loadSessions").onclick = loadSessions;
  document.getElementById("backToSessions").onclick = () => {
    document.getElementById("sessionDetails").style.display = "none";
    document.getElementById("sessionList").style.display = "block";
  };
})();
