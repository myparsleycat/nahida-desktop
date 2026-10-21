// Reduced compliant slit outlet, not a resolved solid/fluid contact solver.
// Two overlapping lateral plates restrict the right axis. The long slit axis
// lies front/back, perpendicular to the downward outlet axis.
struct SoftNozzle {
 float pressure;float opening;float speed;
 float halfGap;float halfLength;float lateralFan;float longitudinalFan;
 float lateralBias;float sheetAmount;
};
float nozzlePressure(float setting,float pulseSpeed){
 // Low-2/3 rise to the low-1 pressure reference during a pulse, then relax.
 float pulseBlend=smoothstep(0,.45,saturate((pulseSpeed-1)/.85));
 float effectiveSetting=lerp(setting,max(setting,1.1275),pulseBlend);
 return clamp(effectiveSetting,.28,3.94)*pow(clamp(pulseSpeed,1,2.2),2);
}
float advanceNozzleOpening(float previous,float pressure,float dt){
 float target=clamp(.65+.35*sqrt(pressure),.80,1.75);
 // Opening responds over 55 ms; overlapping plates relax more slowly (90 ms).
 float response=target>previous?.055:.090;
 return lerp(previous,target,1-exp(-clamp(dt,0,.067)/response));
}
SoftNozzle evaluateSoftNozzle(float setting,float pulseSpeed,float previous,float dt,float t){
 SoftNozzle n;n.pressure=nozzlePressure(setting,pulseSpeed);
 n.opening=advanceNozzleOpening(previous,n.pressure,dt);
 // Illustrative dimensions only; the source remains within its existing socket.
 n.halfGap=.00065*n.opening;
 n.halfLength=.0020*(1+.025*sin(t*3.1));
 float discharge=.78+.22*saturate((n.opening-.7)/.7);
 float referenceDischarge=.78+.22*(.3/.7);
 float ripple=max(.8,1+.08*sin(t*6.3)+.035*sin(t*19.7+1.2));
 float drive=smoothstep(1.69,3.94,clamp(setting,.28,3.94));
 float pulseDrive=saturate((pulseSpeed-1)/.85);
 float jetBoost=1+.35*max(drive,pulseDrive);
 n.speed=.3807886553*sqrt(n.pressure*ripple)*discharge/referenceDischarge*jetBoost;
 float pressureSheet=saturate((setting-1.69)/2.25);
 float pulseSheet=saturate((pulseSpeed-1)/.85);
 n.sheetAmount=max(pressureSheet,pulseSheet);
 float contact=saturate((1.25-n.opening)/.45);
 // Symmetric thin fan: up to 10 degrees each way along the long slit axis.
 n.lateralFan=lerp(.012,.006,n.sheetAmount);
 n.longitudinalFan=lerp(.035,.17632698,n.sheetAmount);
 n.lateralBias=(.006*sin(t*7.1)+.003*sin(t*17.3+1.4))*contact*(1-n.sheetAmount);
 return n;
}

// A narrow round outlet (Slots.hlsl NOZZLE_JET): one coherent straight stream,
// 3 m/s at the default force and up to 5 m/s under raised force or a pulse,
// without the slit's sheet, flutter or low-pressure breakup. The flow-trajectory
// preview in the editor repeats this speed.
static const float JET_SPEED_REFERENCE=3.0,JET_SPEED_MIN=.8,JET_SPEED_MAX=5.0;
static const float JET_FAN=.004,JET_RADIUS=.0006;
void shapeJetNozzle(inout SoftNozzle n){
 n.speed=clamp(JET_SPEED_REFERENCE*sqrt(n.pressure/1.69),JET_SPEED_MIN,JET_SPEED_MAX);
 n.halfGap=JET_RADIUS*n.opening;n.halfLength=n.halfGap;
 n.lateralFan=JET_FAN;n.longitudinalFan=JET_FAN;n.lateralBias=0;n.sheetAmount=0;
}

// Artistic near-field jet response: normal gravity returns downstream.
// Use frozen launch speed, not the falling speed, so gravity cannot weaken itself.
float jetGravityScale(float launchSpeed,float travel){
 float speedWeight=smoothstep(.40,1.20,max(launchSpeed,0));
 float nearField=1-smoothstep(.12,.30,max(travel,0));
 return lerp(1,.55,speedWeight*nearField);
}

// Low pressure loses its coherent core; pulse pressure temporarily restores it.
float lowFlowScatter(float setting,float pulseSpeed){
 return (1-smoothstep(.28,1.69,setting))*(1-saturate((pulseSpeed-1)/.85));
}
