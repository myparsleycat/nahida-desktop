Texture1D<float4> IniParams:register(t120);
RWStructuredBuffer<float4> pulse:register(u0);
uint mixBits(uint x){x^=x>>16;x*=0x7feb352du;x^=x>>15;x*=0x846ca68bu;x^=x>>16;return x;}
float random01(uint seed){return float(mixBits(seed)>>8)/16777216.;}
void beginPulse(float t,float rise,float hold,float fall,float amplitude,uint seq){
 float duration=rise+hold+fall;
 pulse[3]=float4(rise,hold,fall,0);
 pulse[0]=float4(t+duration+1.6+3.2*random01(seq*53+2),t,duration,amplitude);
 pulse[1].y=seq;
}
[numthreads(1,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 float t=IniParams[239].y+IniParams[239].x,request=IniParams[237].w;
 float toggleRequest=IniParams[240].w;
 bool requested=IniParams[240].z>.5;
 if(pulse[2].w<.5){
  pulse[0]=float4(t+2,-100,.4,.65);
  pulse[1]=float4(request,0,t,toggleRequest>0?toggleRequest-1:0);
  pulse[2]=float4(1,0,0,1);pulse[3]=float4(.2,0,.2,0);
  pulse[4]=0;pulse[5]=float4(1,0,0,0);
 }
 pulse[1].x=request;
 bool toggled=toggleRequest!=pulse[1].w;pulse[1].w=toggleRequest;
 float age=t-pulse[0].y;
 bool active=age>=0 && age<pulse[0].z;
 bool automatic=IniParams[239].w>.5;
 if(!automatic)pulse[0].x=t+2;
 if(toggled || (requested && !active && (automatic && t>=pulse[0].x))){
  uint seq=(uint)pulse[1].y+1,seed=seq*747796405u+asuint(t);
  float scale=toggled?(requested?.6:.5):1;
  float rise=(.14+.15*random01(seed+1))*scale;
  float hold=.5*random01(seed+4)*scale;
  float fall=(.14+.15*random01(seed+5))*scale;
  if(toggled && requested){
   // Keep the onset/hold timing; release to baseline takes three times longer.
   fall+=rise*.2+hold*.4;rise*=.8;hold*=.6;
   fall*=3;
   // Double the complete on-pulse while preserving its envelope proportions.
   rise*=2;hold*=2;fall*=2;
  }
  if(toggled && !requested){
   // Initial off pulse: twice its former total time sustaining, then an
   // half-as-long fade. Subsequent pulses retain their shorter timing.
   float sustain=2*(rise+hold+fall);
   hold=max(0,sustain-rise);fall=sustain*.5;
   rise*=.35;hold*=.35;fall*=.35;
   // Slower onset without extending the complete off pulse or its release.
   float extraRise=min(rise*2,hold);
   rise+=extraRise;hold-=extraRise;
  }
  beginPulse(t,rise,hold,fall,.85+.35*random01(seed+3),seq);
  pulse[5].x=1;
  pulse[5].z=(toggled && requested)?1:0;
  if(toggled){
   pulse[4]=requested?float4(0,0,0,0):float4(1,t+pulse[0].z+.60,0,floor(4*random01(seed+8)));
   // Negative z stores the independently selected total off-pulse count.
   if(!requested)pulse[5].z=-(1+floor(3*random01(seed+9)));
  }
 }else if(!requested){
  // Select one to three total diminishing off pulses, including the first.
  if(pulse[4].x>=1 && pulse[4].x<=3 && t>=pulse[4].y){
   pulse[4].x+=1;
   if(pulse[4].x<=-pulse[5].z){
    float duration=pulse[0].z*(pulse[4].x==2?(.55/1.05):.55);
    beginPulse(t,duration*.3,duration*.1,duration*.6,pulse[0].w*.5,(uint)pulse[1].y+1);
    pulse[4].y=t+duration+(pulse[4].x==2?.45:.70);
   }else{
    pulse[0].y=-100;pulse[4].y=t;
    pulse[4].x=pulse[4].w>0?4:5;
   }
  }
  // Finish with zero to three sputters, separated by progressively longer gaps.
  if(pulse[4].x==4){
   if(pulse[4].z<pulse[4].w && t>=pulse[4].y){
    uint seq=(uint)pulse[1].y+1;
    // Only the final sputter keeps its short duration.
    float sputterScale=(pulse[4].z+1<pulse[4].w)?2:1;
    beginPulse(t,.015*sputterScale,(.025+.02*random01(seq+31))*sputterScale,.025*sputterScale,.05,seq);
    pulse[4].z+=1;
    float maximumGap=.95+.25*(pulse[4].z-1);
    float minimumGap=maximumGap*.98;
    pulse[4].y=t+pulse[0].z+lerp(minimumGap,maximumGap,random01(seq+32));
    pulse[5].x=.5;
   }
   if(pulse[4].z>=pulse[4].w && t>=pulse[0].y+pulse[0].z)pulse[4].x=5;
  }
 }
 age=t-pulse[0].y;active=age>=0 && age<pulse[0].z;
 float envelope=0;
 if(active){
  if(age<pulse[3].x)envelope=smoothstep(0,pulse[3].x,age);
  else if(age<pulse[3].x+pulse[3].y)envelope=1;
  else envelope=1-smoothstep(0,pulse[3].z,age-pulse[3].x-pulse[3].y);
 }
 // Manual hold has its own envelope: no repeat/cooldown, sustain until release.
 float elapsed=clamp(t-pulse[1].z,0,.067);
 float manualLevel=requested?pulse[5].y:0;
 if(requested){
  manualLevel=saturate(manualLevel+(request>.5?elapsed/.18:-elapsed/.35));
 }
 pulse[5].y=manualLevel;
 float manualEnvelope=smoothstep(0,1,manualLevel);
 bool shuttingDown=pulse[4].x>=1 && pulse[4].x<=4;
 pulse[3].w=(requested || (shuttingDown && active))?1:0;
 float prevAge=max(0,pulse[1].z-pulse[0].y),emitted=0;
 if(active && pulse[4].x<4)emitted=clamp(floor(age*160)-floor(prevAge*160),0,8);
 if(requested && manualEnvelope>.001){
  float manualDrops=clamp(floor(t*160)-floor(pulse[1].z*160),0,8)*manualEnvelope;
  emitted=max(emitted,manualDrops);
 }
 // The on-pulse's peripheral spray fades with its pressure envelope.
 float onBlend=max(envelope,manualEnvelope);
 if(pulse[5].z>.5){
  emitted=floor(emitted*onBlend+random01(asuint(t)+(uint)pulse[1].y*7919u));
 }
 pulse[5].w=onBlend;
 pulse[2]=float4(1+max(pulse[0].w*envelope,manualEnvelope),pulse[1].y,emitted,1);
 if(!active && pulse[5].z>.5)pulse[5].z=0;
 pulse[1].z=t;
}
