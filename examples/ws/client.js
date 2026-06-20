const statusEl = document.getElementById("status");
const messagesEl = document.getElementById("messages");
const formEl = document.getElementById("form");
const inputEl = document.getElementById("input");
const submitBtn = formEl.querySelector("button");

let ws = null;

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

function connect(client_token, beCallbackUrl, chargeUrl) {
  setConnected(false);
  if (ws) {
    ws.close();
  }

  const wsUrl = `${chargeUrl.replace(/^http/, "ws")}/ws?token=${encodeURIComponent(client_token)}&callback_url=${encodeURIComponent(beCallbackUrl)}`;
  ws = new WebSocket(wsUrl);

  ws.addEventListener("open", () => setConnected(true));
  ws.addEventListener("message", (e) => addMessage(e.data));
  ws.addEventListener("close", () => {
    setConnected(false);
    setTimeout(() => connect(client_token, beCallbackUrl, chargeUrl), 1000);
  });
  ws.addEventListener("error", () => {
    setConnected(false);
  });
}

formEl.addEventListener("submit", function (e) {
  e.preventDefault();
  const msg = inputEl.value.trim();
  if (!msg) return;
  inputEl.value = "";
  ws.send(msg);
});

const beData = await (await fetch("/connect")).json();
connect(beData.client_token, beData.callback_url, beData.charge_url);
