import socketio
import time

KUMA_URL = "https://uptime.vandijke.xyz"
API_KEY = "uk2_u0JZ3sHqhj7tRRu-kjjA3mi8flv-HDljLY6sniK9"

# Create a Socket.io client
sio = socketio.Client()

# Event handlers
@sio.event
def connect():
    print("Connected to Uptime Kuma Socket.io")

@sio.event
def disconnect():
    print("Disconnected from Uptime Kuma Socket.io")

def test_connection():
    try:
        # Pass API key in auth object
        print(f"Connecting to {KUMA_URL} with API Key...")
        sio.connect(KUMA_URL, auth={'token': API_KEY}, transports=['websocket', 'polling'])
        
        # Once connected, try to get monitors
        response_data = None
        def on_response(data):
            nonlocal response_data
            response_data = data
            print(f"Monitor list response: {len(data) if isinstance(data, list) else data}")

        print("Requesting monitor list...")
        sio.emit("getMonitorList", callback=on_response)
        
        # Wait for response
        start_time = time.time()
        while response_data is None and time.time() - start_time < 10:
            time.sleep(0.1)
            
        if response_data:
            print("Successfully fetched monitor list using API Key in auth!")
        else:
            print("Failed to fetch monitor list (timeout or empty response)")
            
        sio.disconnect()
    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    test_connection()
