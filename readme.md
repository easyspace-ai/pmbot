# Polymarket 15min BTC Trading Bot - Control System

Based on the "Brain System" design philosophy, this is a continuous control system for the Polymarket 15-minute BTC market.

## Architecture

- **Signal Layer**: Converts raw market data into `Entropy`, `Velocity`, and `Acceleration`.
- **Brain (Controller)**: A continuous control loop that outputs directional intent and risk exposure.
  - **Dual-Channel**: Mixes `Normal` (steady state) and `Shock` (extreme event) control logic.
  - **Auto-Gain**: Adaptively scales risk based on market entropy and velocity.
  - **Time Pressure**: Automatically reduces risk as the event deadline approaches.
- **Safety Layer**:
  - **Freeze Detector**: Stops trading when consensus is reached (Probability > 95% or < 5%).
  - **Kill Switch**: System-level emergency stop.

## Project Structure

```
/cmd/bot       # Main entry point (Simulation)
/pkg/brain     # Core control logic
/pkg/signal    # Signal processing
/pkg/market    # Market data interface (Mocked)
/pkg/safety    # Safety mechanisms
/pkg/types     # Shared data structures
```

## Running the Simulation

```bash
go run cmd/bot/main.go
```

The output shows the internal state of the Brain, including the calculated entropy, velocity, and the resulting trading intent (Up/Down weight and target exposure).
