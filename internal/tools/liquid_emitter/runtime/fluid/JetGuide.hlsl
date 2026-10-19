// A world-advected parametric sheet followed by free ballistic rays.
struct JetGuide {float4 originBirth;float4 axisSpeed;float4 longLane;float4 carrierAmount;};
void evaluateJetGuide(JetGuide guide,float age,float focusDistance,float3 gravity,
 out float3 position,out float3 velocity,out float travel){
 float3 axis=guide.axisSpeed.xyz,longitudinal=guide.longLane.xyz;
 float3 normal=cross(axis,longitudinal);normal/=max(length(normal),1e-6);
 float speed=guide.axisSpeed.w,amount=guide.carrierAmount.w;
 float acceleration=max(0,dot(gravity,axis));
 float focusAge=2*focusDistance/max(speed+sqrt(speed*speed+2*acceleration*focusDistance),1e-6);
 float t=min(age,focusAge),distance=speed*t+.5*acceleration*t*t;
 float s=saturate(distance/focusDistance),ds=(speed+acceleration*t)/focusDistance;
 float lane=guide.longLane.w;
 float sideLane=frac(sin(guide.originBirth.w*71.3+lane*17.1)*43758.5453)*2-1;
 float width=.14*focusDistance*amount;
 float thickness=.025*focusDistance*amount;
 // Pointed, smoothly widening and narrowing leaf, with a small coherent ripple.
 float phase=s*12+guide.originBirth.w*3.5;
 float ripple=1;
 float profile=4*s*(1-s),derivative=4*(1-2*s);
 float offset=lane*width*profile*ripple;
 float slope=lane*width*(derivative*ripple)*ds;
 float side=sideLane*thickness*sin(3.14159265*s);
 float sideSpeed=sideLane*thickness*3.14159265*cos(3.14159265*s)*ds;
 position=guide.originBirth.xyz+guide.carrierAmount.xyz*t+axis*(speed*t)+.5*gravity*t*t+longitudinal*offset+normal*side;
 velocity=guide.carrierAmount.xyz+axis*speed+gravity*t+longitudinal*slope+normal*sideSpeed;
 travel=distance;
 if(age>focusAge){
  float remainder=age-focusAge;
  float3 displacement=velocity*remainder+.5*gravity*remainder*remainder;
  position+=displacement;velocity+=gravity*remainder;
  travel=focusDistance+length(displacement);
 }
}
