const statusEl = document.getElementById("status");
const messagesEl = document.getElementById("messages");
const formEl = document.getElementById("form");
const inputEl = document.getElementById("input");
const submitBtn = formEl.querySelector("button");

function setStatus(text) {
  statusEl.textContent = text;
}

function addMessage(text) {
  const li = document.createElement("li");
  li.textContent = text;
  messagesEl.appendChild(li);
}

function setConnected(connected) {
  statusEl.textContent = connected ? "Connected" : "Connecting...";
  inputEl.disabled = !connected;
  submitBtn.disabled = !connected;
}

// Connect to the SSE endpoint and handle messages
function connect(client_token, beCallbackUrl, chargeUrl) {
  setConnected(false);

  const url = `${chargeUrl}/sse?token=${encodeURIComponent(client_token)}&callback_url=${encodeURIComponent(beCallbackUrl)}`;
  const eventSource = new EventSource(url);

  eventSource.onopen = () => setConnected(true);
  eventSource.onmessage = (e) => addMessage(e.data);
  eventSource.onerror = () => {
    setConnected(false);
    eventSource.close();

    // Attempt to reconnect after a delay
    setTimeout(() => connect(client_token, beCallbackUrl, chargeUrl), 1000);
  };
}

async function sendMessage(msg) {}

formEl.addEventListener("submit", async function (e) {
  e.preventDefault();
  const msg = inputEl.value.trim();
  inputEl.value = "";
  await sendMessage(msg);

  if (!msg) return;

  // Send the message to the backend
  const res = await fetch(`/sendMessage`, {
    method: "POST",
    body: msg,
  });
  if (!res.ok) {
    console.log("Failed to send message:", res.statusText);
  }
});

const beData = await (await fetch("/connect")).json();
connect(beData.client_token, beData.callback_url, beData.charge_url);
