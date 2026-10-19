// Shared solver/render profile, frozen from pressure at particle birth.
float sheetFocusDistance(float amount){
 return lerp(.035,.100,smoothstep(0,1,saturate(amount)));
}
