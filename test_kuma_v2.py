import socketio
import time

sio = socketio.Client()

KUMA_URL = "https://uptime.vandijke.xyz"
API_KEY = "uk2_u0JZ3sHqhj7tRRu-kjjA3mi8flv-HDljLY6sniK9"

@sio.event
def connect():
    print("Connected to Socket.io server")

@sio.event
def disconnect():
    print("Disconnected from server")

def test_login():
    try:
        sio.connect(KUMA_URL, transports=['websocket', 'polling'])
        
        login_response = {}
        def on_login_response(data):
            nonlocal login_response
            login_response = data
            print(f"Login response received: {data}")

        print("Emitting loginByApiKey...")
        sio.emit("loginByApiKey", API_KEY, callback=on_login_response)
        
        # Wait for response
        start_time = time.time()
        while not login_response and time.time() - start_time < 10:
            time.sleep(0.1)
            
        if login_response.get("ok"):
            print("Login successful!")
            
            monitors = []
            def on_monitor_list(data):
                nonlocal monitors
                monitors = data
                print(f"Received {len(data) if isinstance(data, list) else 0} monitors")

            sio.emit("getMonitorList", callback=on_monitor_list)
            
            start_time = time.time()
            while not monitors and time.time() - start_time < 10:
                time.sleep(0.1)
        else:
            print(f"Login failed: {login_response}")
            
        sio.disconnect()
    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    test_login()
