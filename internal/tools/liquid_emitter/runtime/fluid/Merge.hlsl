// Clinging drops of one emitter that touch merge into one larger drop, which
// runs faster and leaves a trail, so liquid fed to one spot gathers and runs
// off as a rivulet instead of a row of beads. SplashMerge.hlsl picks for
// each drop the nearest one it touches, on the snapshot SplashUpdate left;
// SplashSpawn merges only two drops that picked each other, so no drop is
// taken twice in a frame or passed along a chain.
// Includers declare FluidParticle, source and contactHistory first.
#ifndef LIQUID_MERGE
#define LIQUID_MERGE
static const uint NO_MERGE=0xffffffffu;
// Center distance per unit of the two sizes at which drops touch: the drawn
// lens radius of a size 1 drop (SURFACE_RADIUS).
static const float MERGE_REACH=.0045;
// Drops facing apart, as on two sides of a thin limb, do not touch.
static const float MERGE_MIN_NORMAL=.5;
// The largest drop a merge makes; the rest stays with the other drop.
static const float MERGE_MAX_SCALE=1.8,MERGE_MAX_VOLUME=MERGE_MAX_SCALE*MERGE_MAX_SCALE*MERGE_MAX_SCALE;
static const float MERGE_REST_MIN=.001;
bool clingingDrop(FluidParticle p,uint i){
 return i<AIRBORNE_SPLASH_COUNT&&p.velocityAlive.w>.5&&p.predictedLambda.w>.5
  &&p.densityBirth.w>3.5&&!reservedContact(p.densityBirth.w);
}
// A drop held on a surface it cannot see does not merge until it sees it.
bool mergeable(FluidParticle p,uint i){
 return clingingDrop(p,i)&&contactHistory[i*CONTACT_ROWS+2].w<.5;
}
uint mergeCandidate(uint i){
 FluidParticle p=source[i];
 if(!mergeable(p,i))return NO_MERGE;
 float scale=p.densityBirth.z,best=1e30;
 uint mode=adhesionMode(p.predictedLambda.w),candidate=NO_MERGE;
 [loop]for(uint j=0;j<AIRBORNE_SPLASH_COUNT;j++){
  if(j==i)continue;
  FluidParticle q=source[j];
  if(q.velocityAlive.w!=p.velocityAlive.w||!mergeable(q,j)||adhesionMode(q.predictedLambda.w)!=mode)continue;
  if(dot(q.predictedLambda.xyz,p.predictedLambda.xyz)<MERGE_MIN_NORMAL)continue;
  float3 offset=q.positionAge.xyz-p.positionAge.xyz;
  float reach=MERGE_REACH*(scale+q.densityBirth.z),distance2=dot(offset,offset);
  if(distance2<reach*reach&&distance2<best){best=distance2;candidate=j;}
 }
 return candidate;
}
#endif
