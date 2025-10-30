import time
from datetime import datetime

# Print completion message with current time
now = datetime.now().strftime("%H:%M")
print(f"Experiment completed at {now}")

# Keep container alive forever
while True:
    time.sleep(3600)  # sleep 1 hour to avoid CPU spin


