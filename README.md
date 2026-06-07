# notman
Tends to your clients persistant connection so that your backend doesn't have to.
Giving you the freedom to run your backend serverlessly while still offering your clients the possibility of live updates.

## Basic flow
```mermaid
sequenceDiagram
    participant Client
    participant notman
    participant Backend

    Note over Client,Backend: === Flow 1: Client Connects via SSE ===

    Client->>notman: GET /sse?token=<client-token>&callback_url=<backend-callback-url>

    notman->>Backend: GET /.well-known/notman-allowed
    Backend-->>notman: Allowed deployment URLs
    Note over notman: Verify this deployment is allowed

    notman->>notman: Generate sendToken (JWT for backend to send messages)

    notman->>Backend: POST <callback_url> (CloudEvent: notman.connected.v1)
    Note over notman,Backend: Body: { clientToken, sendToken, connectionId }
    Note over notman,Backend: Header: Webhook-Signature (detached JWS)
    Backend-->>notman: 200 OK

    notman-->>Client: SSE stream open (text/event-stream)
    Note over Client,notman: Connection is live


    Note over Client,Backend: === Flow 2: Backend Sends Message to Client ===

    Backend->>notman: POST /send?send_token=<sendToken>
    Note over notman: Validate sendToken (JWT: iss, aud, purpose=send)
    notman-->>Client: SSE event: data (message bytes)
    notman-->>Backend: 200 OK


    Note over Client,Backend: === Flow 3: Teardown (Disconnect / Timeout) ===

    alt Client Disconnects
        Client->>notman: Connection closed / context cancelled
    else Max Connection Duration Reached
        Note over notman: Timeout is configurable
    end
    
    notman->>Backend: POST <callback_url> (CloudEvent: notman.disconnected.v1)
    Note over notman,Backend: Body: { connectionId }
    Backend-->>notman: 200 OK
```
