# Generic before/after example

## Before

```ini
[TextureOverrideExample]
hash = 12345678
handling = skip

Resource\RabbitFX\Diffuse = ref ResourceBodyD
Resource\RabbitFX\Lightmap = ref ResourceBodyL
Resource\RabbitFX\Normalmap = ref ResourceBodyN
Resource\RabbitFX\Materialmap = ref ResourceBodyM
run = Commandlist\RabbitFX\SetTextures

drawindexed = 1200, 0, 0

Resource\RabbitFX\Diffuse = ref ResourceDressD
Resource\RabbitFX\Lightmap = ref ResourceDressL
Resource\RabbitFX\Normalmap = ref ResourceDressN
Resource\RabbitFX\Materialmap = ref ResourceDressM
run = Commandlist\RabbitFX\SetTextures

drawindexed = 2400, 1200, 0
```

## After, when local evidence proves N/L/M/D -> t0/t1/t2/t3

```ini
[ResourceRabbitlessTempT0]
[ResourceRabbitlessTempT1]
[ResourceRabbitlessTempT2]
[ResourceRabbitlessTempT3]

[CommandListSaveRabbitlessTextureSlots]
ResourceRabbitlessTempT0 = ref ps-t0
ResourceRabbitlessTempT1 = ref ps-t1
ResourceRabbitlessTempT2 = ref ps-t2
ResourceRabbitlessTempT3 = ref ps-t3

[CommandListRestoreRabbitlessTextureSlots]
ps-t0 = ref ResourceRabbitlessTempT0
ps-t1 = ref ResourceRabbitlessTempT1
ps-t2 = ref ResourceRabbitlessTempT2
ps-t3 = ref ResourceRabbitlessTempT3
ResourceRabbitlessTempT0 = null
ResourceRabbitlessTempT1 = null
ResourceRabbitlessTempT2 = null
ResourceRabbitlessTempT3 = null

[TextureOverrideExample]
hash = 12345678
handling = skip

run = CommandListSaveRabbitlessTextureSlots
ps-t0 = ref ResourceBodyN
ps-t1 = ref ResourceBodyL
ps-t2 = ref ResourceBodyM
ps-t3 = ref ResourceBodyD

drawindexed = 1200, 0, 0

ps-t0 = ref ResourceDressN
ps-t1 = ref ResourceDressL
ps-t2 = ref ResourceDressM
ps-t3 = ref ResourceDressD

drawindexed = 2400, 1200, 0
run = CommandListRestoreRabbitlessTextureSlots
```

The placement of save and restore is part of the transformation. The direct slot mapping must be proven from the actual package rather than copied from this example without evidence.
