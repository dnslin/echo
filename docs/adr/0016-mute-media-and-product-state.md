# Treat mute as both media state and product state

Muting in echo will update both the local LiveKit audio track and the business room state. The client immediately mutes or unmutes its local audio track to prevent accidental transmission, then reports the mute state to the business service over WebSocket so other members can see the correct UI state. Mute remains higher priority than push-to-talk and free-talk: unmuting only permits audio according to the current voice mode, while muting always prevents audio from being sent.
