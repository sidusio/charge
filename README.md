# Chargé [(d'affaires)](https://en.wikipedia.org/wiki/Charg%C3%A9_d'affaires) - Keeping your clients connected, while you are away.
Chargé tends to your clients' long-lived connections while you are away, giving you the freedom to run your service serverlessly while still offering your clients the possibility of e.g. live updates.

## Basic flow
```mermaid
sequenceDiagram
    participant Client
    participant charge as Chargé
    participant Backend

    Note over Client,Backend: === Flow 1: Client Connects ===

    Client->>charge: GET /sse or WS upgrade (/ws)
    Note over Client,charge: token & callback_url query params

    charge->>Backend: GET /.well-known/charge-allowed
    Backend-->>charge: Allowed deployment URLs
    Note over charge: Verify this deployment is allowed

    charge->>charge: Generate sendToken (JWT for backend to send messages)

    charge->>Backend: POST <callback_url> (CloudEvent: charge.connected.v1)
    Note over charge,Backend: Body: { clientToken, sendToken, connectionId }
    Note over charge,Backend: Header: Webhook-Signature (detached JWS)
    Backend-->>charge: 200 OK

    charge-->>Client: SSE stream / WebSocket connection open
    Note over Client,charge: Connection is live


    Note over Client,Backend: === Flow 2: Backend Sends Message to Client ===

    Backend->>charge: POST /send?send_token=<sendToken>
    Note over charge: Validate sendToken (JWT)
    charge-->>Client: SSE event / WebSocket message (message bytes)
    charge-->>Backend: 200 OK


    Note over Client,Backend: === Flow 3: Client Sends Message (WebSocket) ===

    Client->>charge: WebSocket message (message bytes)
    charge->>Backend: POST <callback_url> (CloudEvent: charge.client.message.v1)
    Note over charge,Backend: Body: { connectionId, data }
    Backend-->>charge: 200 OK


    Note over Client,Backend: === Flow 4: Teardown (Disconnect / Timeout) ===

    alt Client Disconnects
        Client->>charge: Connection closed / context cancelled
    else Max Connection Duration Reached
        Note over charge: Timeout is configurable
    end
    
    charge->>Backend: POST <callback_url> (CloudEvent: charge.disconnected.v1)
    Note over charge,Backend: Body: { connectionId }
    Backend-->>charge: 200 OK
```
