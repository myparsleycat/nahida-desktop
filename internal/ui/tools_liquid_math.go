package ui

import "math"

// liquidAngles are a direction's degrees away from straight down (0 down, 90 horizontal, 180 up) and its
// degrees counterclockwise seen from above, from the mesh's horizontal X axis.
type liquidAngles struct {
	tilt, azimuth float64
}

// liquidFrame returns down, the horizontal part of the mesh's X axis (Z when down lies along X), and the axis
// a quarter turn counterclockwise from it seen from above.
func liquidFrame(down vec3) (axis, side, across vec3) {
	axis = liquidNormalize(down)
	reference := vec3{1, 0, 0}
	if math.Abs(axis[0]) > 0.9 {
		reference = vec3{0, 0, 1}
	}
	side = liquidNormalize(reference.sub(axis.scale(reference.dot(axis))))
	return axis, side, side.cross(axis)
}

func liquidDirectionToAngles(direction, down vec3) liquidAngles {
	axis, side, across := liquidFrame(down)
	unit := liquidNormalize(direction)
	tilt := math.Acos(math.Min(math.Max(unit.dot(axis), -1), 1)) * 180 / math.Pi
	azimuth := math.Atan2(unit.dot(across), unit.dot(side)) * 180 / math.Pi
	return liquidAngles{tilt: tilt, azimuth: math.Mod(azimuth+360, 360)}
}

func liquidAnglesToDirection(angles liquidAngles, down vec3) vec3 {
	axis, side, across := liquidFrame(down)
	tilt, azimuth := angles.tilt*math.Pi/180, angles.azimuth*math.Pi/180
	horizontal := side.scale(math.Cos(azimuth)).add(across.scale(math.Sin(azimuth)))
	return axis.scale(math.Cos(tilt)).add(horizontal.scale(math.Sin(tilt)))
}

// liquidNormalize is the React port's normalize, which leaves a zero vector at zero instead of failing.
func liquidNormalize(v vec3) vec3 {
	return v.scale(1 / math.Max(v.length(), 1e-10))
}

// liquidLimitOffsetAxis clamps only the edited axis to the length limit, so the other axes keep their values.
func liquidLimitOffsetAxis(offset vec3, axis int, limit float64) vec3 {
	var others float64
	for i, value := range offset {
		if i != axis {
			others += value * value
		}
	}
	room := math.Sqrt(math.Max(limit*limit-others, 0))
	offset[axis] = math.Min(math.Max(offset[axis], -room), room)
	return offset
}

// liquidTrajectory is the mean path of an emitter's stream, sampled with the time each point is reached.
type liquidTrajectory struct {
	points   []vec3
	times    []float64
	lifetime float64
}

func liquidSmooth(edge0, edge1, x float64) float64 {
	t := math.Min(math.Max((x-edge0)/(edge1-edge0), 0), 1)
	return t * t * (3 - 2*t)
}

// liquidFlowTrajectory is the path of an emitter's stream in meters in source mesh space, where gravity
// pulls along down, ported from LiquidGravityProbe's AttachmentEditor. Force 1 is the runtime's former default
// strength (1.69), as in Slots.hlsl slotDrive. The spray nozzle includes the nozzle exit correction, the
// high-pressure jet boost and near-field gravity relief; the jet nozzle's speed follows SoftNozzle.hlsl
// shapeJetNozzle under plain gravity. Collision, motion, pulses and particle interaction are ignored.
func liquidFlowTrajectory(point, offset, direction, down vec3, force float64, jet bool) liquidTrajectory {
	setting := math.Min(math.Max(1.69*force, 0.28), 3.94)
	const lifetime, step = 0.48, 1.0 / 240
	speed := liquidSprayNozzleSpeed(setting)
	if jet {
		speed = math.Min(math.Max(3*math.Sqrt(setting/1.69), 0.8), 5)
	}

	dir := liquidNormalize(direction)
	fall := liquidNormalize(down)
	up := dir.scale(-1)
	reference := vec3{0, -1, 0}
	if math.Abs(up.dot(reference)) > 0.95 {
		reference = vec3{1, 0, 0}
	}
	right := liquidNormalize(up.cross(reference))
	forward := liquidNormalize(right.cross(up))

	position := point.add(offset).add(dir.scale(0.004)).add(forward.scale(-0.001))
	velocity := dir.scale(speed)
	travel := 0.0
	trajectory := liquidTrajectory{points: []vec3{position}, times: []float64{0}, lifetime: lifetime}
	for time := 0.0; time < lifetime-1e-9; {
		h := math.Min(step, lifetime-time)
		gravity := 9.81
		if !jet {
			gravity *= 1 - 0.45*liquidSmooth(0.4, 1.2, speed)*(1-liquidSmooth(0.12, 0.3, travel))
		}
		next := position.add(velocity.scale(h)).add(fall.scale(0.5 * gravity * h * h))
		velocity = velocity.add(fall.scale(gravity * h))
		travel += next.sub(position).length()
		position = next
		time += h
		trajectory.points = append(trajectory.points, position)
		trajectory.times = append(trajectory.times, time)
	}
	return trajectory
}

func liquidSprayNozzleSpeed(setting float64) float64 {
	opening := math.Min(math.Max(0.65+0.35*math.Sqrt(setting), 0.8), 1.75)
	discharge := 0.78 + 0.22*math.Min(math.Max((opening-0.7)/0.7, 0), 1)
	jetBoost := 1 + 0.35*liquidSmooth(1.69, 3.94, setting)
	return 0.3807886553 * math.Sqrt(setting) * discharge * jetBoost / (0.78 + 0.22*(0.3/0.7))
}

// pointAt is where a tracer that left the nozzle age seconds ago is.
func (t liquidTrajectory) pointAt(age float64) vec3 {
	index := 0
	for index < len(t.times)-2 && t.times[index+1] < age {
		index++
	}
	span := t.times[index+1] - t.times[index]
	fraction := 0.0
	if span > 0 {
		fraction = math.Min(math.Max((age-t.times[index])/span, 0), 1)
	}
	return t.points[index].add(t.points[index+1].sub(t.points[index]).scale(fraction))
}
