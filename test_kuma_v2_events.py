import socketio
import time

KUMA_URL = "https://uptime.vandijke.xyz"
API_KEY = "uk2_u0JZ3sHqhj7tRRu-kjjA3mi8flv-HDljLY6sniK9"

sio = socketio.Client()

authenticated = False

@sio.event
def connect():
    print("Connected")
    sio.emit("loginByApiKey", API_KEY)

@sio.on("loginByApiKeyResponse")
def on_login_response(data):
    global authenticated
    print(f"Login Response: {data}")
    if data.get("ok"):
        authenticated = True

@sio.on("authenticated")
def on_authenticated():
    global authenticated
    print("Authenticated event received!")
    authenticated = True

def test():
    try:
        sio.connect(KUMA_URL)
        time.sleep(2)
        if authenticated:
            print("Auth successful")
            sio.emit("getMonitorList", callback=lambda d: print(f"Monitors: {len(d)}"))
            time.sleep(2)
        else:
            print("Auth failed or timed out")
        sio.disconnect()
    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    test()
