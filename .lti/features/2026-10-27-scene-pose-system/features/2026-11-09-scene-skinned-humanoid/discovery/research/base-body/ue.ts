// Unreal mannequin names (Quaternius Universal Base Characters) to the standard humanoid.
export const ueTable: Record<string, string> = {
  pelvis: 'hips', spine_01: 'spine', spine_02: 'chest', spine_03: 'upperChest', neck_01: 'neck', Head: 'head',
  ...Object.fromEntries(['l', 'r'].flatMap((x) => {
    const s = x === 'l' ? 'left' : 'right'
    return [
      [`clavicle_${x}`, `${s}Shoulder`], [`upperarm_${x}`, `${s}UpperArm`], [`lowerarm_${x}`, `${s}LowerArm`], [`hand_${x}`, `${s}Hand`],
      [`thigh_${x}`, `${s}UpperLeg`], [`calf_${x}`, `${s}LowerLeg`], [`foot_${x}`, `${s}Foot`], [`ball_${x}`, `${s}Toes`],
      [`thumb_01_${x}`, `${s}ThumbMetacarpal`], [`thumb_02_${x}`, `${s}ThumbProximal`], [`thumb_03_${x}`, `${s}ThumbDistal`],
      ...(['index', 'middle', 'ring', 'pinky'] as const).flatMap((f) => {
        const F = f === 'pinky' ? 'Little' : f[0]!.toUpperCase() + f.slice(1)
        return [[`${f}_01_${x}`, `${s}${F}Proximal`], [`${f}_02_${x}`, `${s}${F}Intermediate`], [`${f}_03_${x}`, `${s}${F}Distal`]]
      }),
    ]
  })),
}
