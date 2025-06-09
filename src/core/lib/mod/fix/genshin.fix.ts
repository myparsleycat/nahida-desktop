import * as fs from 'fs';
import * as path from 'path';

interface RemapData {
    name: string;
    mapping: number[];
}

interface ProcessingConfig {
    fix_41: boolean;
    fix_43: boolean;
    fix_44_47: boolean;
    fix_54: boolean;
    oldvsnew_41: Record<string, string>;
    oldvsnew_43: Record<string, string>;
    old_vs_new: Record<string, string>;
    old_vs_new_54: Record<string, Record<string, Record<string, string>>>;
    reverse: boolean;
    no_remap: boolean;
}

interface ProcessingResult {
    updated: boolean;
    log: string[];
}

interface JsonData41 {
    old_draw_vb: string;
    new_draw_vb: string;
    path: string;
    position_vb: string;
}

interface JsonData43 {
    old_ib: string;
    new_ib: string;
    path: string;
}

const old_vs_new_54: Record<string, Record<string, Record<string, string>>> = {
    Characters: {
        Mavuika: {
            "4e654846": "00088f8d" // blend_vb
        },
        MavuikaHair: {
            "2246e755": "64424fcd" // blend_vb
        },
        MavuikaFlameHair: {
            "834cea18": "b467ca3e", // position_vb
            "8b254dde": "2126e820", // blend_vb
            "02c6dd96": "35df319d", // texcoord_Vb
            "5bee95b2": "59eb991a"  // ib
        },
        Xilonen: {
            "b2b73dba": "ded724d2"
        }
    }
};

// MARK: alljson_41
const alljson_41: JsonData41[] = [
    {
        old_draw_vb: "a3dd8fae",
        new_draw_vb: "",
        path: "./EnemyData/AbyssHerald/hash.json",
        position_vb: "73df62fe"
    },
    {
        old_draw_vb: "16e5380a",
        new_draw_vb: "01896bdc",
        path: "./EnemyData/AbyssLectorElectro/hash.json",
        position_vb: "385d2fad"
    },
    {
        old_draw_vb: "3e3f6114",
        new_draw_vb: "520d87f7",
        path: "./EnemyData/AbyssLectorPyro/hash.json",
        position_vb: "6db3d14e"
    },
    {
        old_draw_vb: "32c81efa",
        new_draw_vb: "",
        path: "./EnemyData/Andrius/hash.json",
        position_vb: "22ffcdf0"
    },
    {
        old_draw_vb: "e6cd858b",
        new_draw_vb: "6f28aaac",
        path: "./EnemyData/AnnihilationSpecialistMek/hash.json",
        position_vb: "da152664"
    },
    {
        old_draw_vb: "81b66b25",
        new_draw_vb: "",
        path: "./EnemyData/Azhdaha/hash.json",
        position_vb: "f4192e42"
    },
    {
        old_draw_vb: "482ef3dc",
        new_draw_vb: "",
        path: "./EnemyData/BigSlimeDendro/hash.json",
        position_vb: "f0777f4d"
    },
    {
        old_draw_vb: "1c64ef68",
        new_draw_vb: "",
        path: "./EnemyData/BlackSerpentKnightRockBreakerAx/hash.json",
        position_vb: "0d95b913"
    },
    {
        old_draw_vb: "7fd2dc62",
        new_draw_vb: "5db509de",
        path: "./EnemyData/BlackSerpentWindcutter/hash.json",
        position_vb: "e063a51f"
    },
    {
        old_draw_vb: "29618597",
        new_draw_vb: "a084aab0",
        path: "./EnemyData/CryoCicinMage/hash.json",
        position_vb: "81f659d4"
    },
    {
        old_draw_vb: "430c9cbe",
        new_draw_vb: "",
        path: "./EnemyData/Dvalin/hash.json",
        position_vb: "40c0ffab"
    },
    {
        old_draw_vb: "71f8c4c6",
        new_draw_vb: "",
        path: "./EnemyData/ElectroRegisvine/hash.json",
        position_vb: "d5a83455"
    },
    {
        old_draw_vb: "c585c3fb",
        new_draw_vb: "4c60ecdc",
        path: "./EnemyData/FatuiAgent/hash.json",
        position_vb: "23d51a03"
    },
    {
        old_draw_vb: "e94f00aa",
        new_draw_vb: "60aa2f8d",
        path: "./EnemyData/FatuiMirrorMaiden/hash.json",
        position_vb: "ddd038f6"
    },
    {
        old_draw_vb: "baf62f1a",
        new_draw_vb: "3313003d",
        path: "./EnemyData/Hilichurl/hash.json",
        position_vb: "23c24723"
    },
    {
        old_draw_vb: "5081af9b",
        new_draw_vb: "d96480bc",
        path: "./EnemyData/HilicurlRangerHydro/hash.json",
        position_vb: "69932870"
    },
    {
        old_draw_vb: "70cc2de5",
        new_draw_vb: "",
        path: "./EnemyData/KairagiFiery/hash.json",
        position_vb: "ecda2927"
    },
    {
        old_draw_vb: "9bc73737",
        new_draw_vb: "",
        path: "./EnemyData/KairagiThunder/hash.json",
        position_vb: "da804958"
    },
    {
        old_draw_vb: "0dcd3f88",
        new_draw_vb: "",
        path: "./EnemyData/LaSignoraHarbingers/hash.json",
        position_vb: "0a894825"
    },
    {
        old_draw_vb: "8edfcd7f",
        new_draw_vb: "073ae258",
        path: "./EnemyData/LawachurlGeo/hash.json",
        position_vb: "715e04f5"
    },
    {
        old_draw_vb: "a191e7c2",
        new_draw_vb: "",
        path: "./EnemyData/MagatsuMitakeNarukaminoMikoto/hash.json",
        position_vb: "05cbdcc5"
    },
    {
        old_draw_vb: "7274e051",
        new_draw_vb: "22758a26",
        path: "./EnemyData/Mitachurl/hash.json",
        position_vb: "a86a5c2b"
    },
    {
        old_draw_vb: "0b983685",
        new_draw_vb: "7a5fa46e",
        path: "./EnemyData/RuinGuard/hash.json",
        position_vb: "73350cc0"
    },
    {
        old_draw_vb: "8b79cbfa",
        new_draw_vb: "babcd593",
        path: "./EnemyData/RuinHunter/hash.json",
        position_vb: "34f98d8f"
    },
    {
        old_draw_vb: "4258388c",
        new_draw_vb: "",
        path: "./EnemyData/TartagliaDelusion/hash.json",
        position_vb: "7b476b00"
    },
    {
        old_draw_vb: "45404603",
        new_draw_vb: "",
        path: "./EnemyData/TartagliaHarbingers/hash.json",
        position_vb: "09eccc99"
    },
    {
        old_draw_vb: "19ca0e6e",
        new_draw_vb: "902f2149",
        path: "./MiscellaneousData/BlackCat/hash.json",
        position_vb: "be82fa92"
    },
    {
        old_draw_vb: "b2bc717f",
        new_draw_vb: "3b595e58",
        path: "./MiscellaneousData/LoginGate/hash.json",
        position_vb: ""
    },
    {
        old_draw_vb: "3a0fd3a8",
        new_draw_vb: "b3eafc8f",
        path: "./MiscellaneousData/CommonChest/hash.json",
        position_vb: "46ef4305"
    },
    {
        old_draw_vb: "9e735f65",
        new_draw_vb: "00c51b76",
        path: "./MiscellaneousData/CryoCicinMageLatern/hash.json",
        position_vb: "1d64b7ef"
    },
    {
        old_draw_vb: "7e46ac01",
        new_draw_vb: "f7a38326",
        path: "./MiscellaneousData/ExquisiteChest/hash.json",
        position_vb: "c426a5b3"
    },
    {
        old_draw_vb: "6b347319",
        new_draw_vb: "e2d15c3e",
        path: "./MiscellaneousData/PreciousChest/hash.json",
        position_vb: "e9239f65"
    },
    {
        old_draw_vb: "6b319e52",
        new_draw_vb: "e2d4b175",
        path: "./MiscellaneousData/Liloupar/hash.json",
        position_vb: "44b721b4"
    },
    {
        old_draw_vb: "10b2ae62",
        new_draw_vb: "99578145",
        path: "./MiscellaneousData/LisaStudentBook/hash.json",
        position_vb: "96f7406a"
    },
    {
        old_draw_vb: "e60a7404",
        new_draw_vb: "6fef5b23",
        path: "./MiscellaneousData/LuxoriousChest/hash.json",
        position_vb: "600d512d"
    },
    {
        old_draw_vb: "9a2e6bfa",
        new_draw_vb: "13cb44dd",
        path: "./MiscellaneousData/SaraWings/hash.json",
        position_vb: "e727e979"
    },
    {
        old_draw_vb: "68f35a77",
        new_draw_vb: "e1167550",
        path: "./MiscellaneousData/ShenheGhost/hash.json",
        position_vb: "0c1b31c0"
    },
    {
        old_draw_vb: "8ab4e0ed",
        new_draw_vb: "",
        path: "./MiscellaneousData/SignoraWhip/hash.json",
        position_vb: "035f91db"
    },
    {
        old_draw_vb: "d7874eb9",
        new_draw_vb: "",
        path: "./MiscellaneousData/TheSevenBarbatos/hash.json",
        position_vb: "031ba068"
    },
    {
        old_draw_vb: "d7874eb9",
        new_draw_vb: "",
        path: "./MiscellaneousData/TheSevenPart/hash.json",
        position_vb: "031ba068"
    },
    {
        old_draw_vb: "11523170",
        new_draw_vb: "98b71e57",
        path: "./MiscellaneousData/Waverider/hash.json",
        position_vb: "a75d82d2"
    },
    {
        old_draw_vb: "0a5aa973",
        new_draw_vb: "",
        path: "./MiscellaneousData/YaeMiko5Tails/hash.json",
        position_vb: "837f3242"
    },
    {
        old_draw_vb: "0dbdd55a",
        new_draw_vb: "",
        path: "./NPCData/Asmoday/hash.json",
        position_vb: "2e8504fd"
    },
    {
        old_draw_vb: "9810aed3",
        new_draw_vb: "11f581f4",
        path: "./EnemyData/EremiteFloralRingDancer/hash.json",
        position_vb: "d2eb04e1"
    },
    {
        old_draw_vb: "1d4867f2",
        new_draw_vb: "94ad48d5",
        path: "./NPCData/BaronBunny/hash.json",
        position_vb: "52cd5d0f"
    },
    {
        old_draw_vb: "3c8597b4",
        new_draw_vb: "b560b893",
        path: "./NPCData/Chubby/hash.json",
        position_vb: "f8bfae50"
    },
    {
        old_draw_vb: "2d132af4",
        new_draw_vb: "8c839d21",
        path: "./NPCData/Crab/Crab/hash.json",
        position_vb: "a883e6be"
    },
    {
        old_draw_vb: "c32d2063",
        new_draw_vb: "",
        path: "./NPCData/Dainsleif/hash.json",
        position_vb: "3c1e8497"
    },
    {
        old_draw_vb: "5b0dca7c",
        new_draw_vb: "",
        path: "./NPCData/Dottore/hash.json",
        position_vb: "3b2689bb"
    },
    {
        old_draw_vb: "f4a61aad",
        new_draw_vb: "7d43358a",
        path: "./NPCData/Guoba/hash.json",
        position_vb: "862aec78"
    },
    {
        old_draw_vb: "52a30508",
        new_draw_vb: "",
        path: "./NPCData/Katheryne/hash.json",
        position_vb: "d5537624"
    },
    {
        old_draw_vb: "ca4b0738",
        new_draw_vb: "",
        path: "./NPCData/LaSignora/hash.json",
        position_vb: "20fc3e49"
    },
    {
        old_draw_vb: "423c6354",
        new_draw_vb: "",
        path: "./NPCData/LoliNahida/hash.json",
        position_vb: "86dba24f"
    },
    {
        old_draw_vb: "61ab7805",
        new_draw_vb: "8ac2cb13",
        path: "./NPCData/Oz/hash.json",
        position_vb: "d5adb4b4"
    },
    {
        old_draw_vb: "cb027e67",
        new_draw_vb: "42e75140",
        path: "./NPCData/Paimon/hash.json",
        position_vb: "1a09e6cd"
    },
    {
        old_draw_vb: "0028d4d1",
        new_draw_vb: "89cdfbf6",
        path: "./NPCData/Paimon/hash.json",
        position_vb: "1f7f0600"
    },
    {
        old_draw_vb: "0dca7cf8",
        new_draw_vb: "",
        path: "./NPCData/RexLapis/hash.json",
        position_vb: "002b6d21"
    },
    {
        old_draw_vb: "e5822bbc",
        new_draw_vb: "",
        path: "./NPCData/Scaramouche/hash.json",
        position_vb: "928af15e"
    },
    {
        old_draw_vb: "896e8a12",
        new_draw_vb: "d458677a",
        path: "./NPCData/Ushi/hash.json",
        position_vb: "4b08d014"
    },
    {
        old_draw_vb: "8747eaa2",
        new_draw_vb: "0ea2c585",
        path: "./PlayerCharacterData/Albedo/hash.json",
        position_vb: "df65bb00"
    },
    {
        old_draw_vb: "70f00d36",
        new_draw_vb: "f9152211",
        path: "./PlayerCharacterData/Alhaitham/hash.json",
        position_vb: "3ef08385"
    },
    {
        old_draw_vb: "c987f4c0",
        new_draw_vb: "4062dbe7",
        path: "./PlayerCharacterData/Aloy/hash.json",
        position_vb: "46de82f3"
    },
    {
        old_draw_vb: "870a7499",
        new_draw_vb: "0eef5bbe",
        path: "./PlayerCharacterData/Amber/hash.json",
        position_vb: "caddc4c6"
    },
    {
        old_draw_vb: "da0adf2f",
        new_draw_vb: "53eff008",
        path: "./PlayerCharacterData/AmberCN/hash.json",
        position_vb: "7f94e8da"
    },
    {
        old_draw_vb: "14c9337a",
        new_draw_vb: "9d2c1c5d",
        path: "./PlayerCharacterData/Ayaka/hash.json",
        position_vb: "0107925f"
    },
    {
        old_draw_vb: "8d173084",
        new_draw_vb: "04f21fa3",
        path: "./PlayerCharacterData/AyakaSpring/hash.json",
        position_vb: "cf78a1d0"
    },
    {
        old_draw_vb: "bf198f17",
        new_draw_vb: "36fca030",
        path: "./PlayerCharacterData/Ayato/hash.json",
        position_vb: "b473c856"
    },
    {
        old_draw_vb: "b1729bce",
        new_draw_vb: "3897b4e9",
        path: "./PlayerCharacterData/Baizhu/hash.json",
        position_vb: "17baa562"
    },
    {
        old_draw_vb: "f41c47cf",
        new_draw_vb: "7df968e8",
        path: "./PlayerCharacterData/Barbara/hash.json",
        position_vb: "85282151"
    },
    {
        old_draw_vb: "60fcbabe",
        new_draw_vb: "e9199599",
        path: "./PlayerCharacterData/BarbaraSummertime/hash.json",
        position_vb: "8b9e7c22"
    },
    {
        old_draw_vb: "927d46ca",
        new_draw_vb: "1b9869ed",
        path: "./PlayerCharacterData/Beidou/hash.json",
        position_vb: "51197c51"
    },
    {
        old_draw_vb: "8b2a1582",
        new_draw_vb: "02cf3aa5",
        path: "./PlayerCharacterData/Bennett/hash.json",
        position_vb: "993d1661"
    },
    {
        old_draw_vb: "d0d995f3",
        new_draw_vb: "593cbad4",
        path: "./PlayerCharacterData/Candace/hash.json",
        position_vb: "9cee8711"
    },
    {
        old_draw_vb: "c11fcbde",
        new_draw_vb: "48fae4f9",
        path: "./PlayerCharacterData/Charlotte/hash.json",
        position_vb: "c5a6d98e"
    },
    {
        old_draw_vb: "7acaf240",
        new_draw_vb: "f32fdd67",
        path: "./PlayerCharacterData/ChildeScarf/hash.json",
        position_vb: "f717e00c"
    },
    {
        old_draw_vb: "796a33a2",
        new_draw_vb: "f08f1c85",
        path: "./PlayerCharacterData/Childe/hash.json",
        position_vb: "f717e00c"
    },
    {
        old_draw_vb: "a50e6bc8",
        new_draw_vb: "2ceb44ef",
        path: "./PlayerCharacterData/Chongyun/hash.json",
        position_vb: "489e3621"
    },
    {
        old_draw_vb: "50e8816e",
        new_draw_vb: "d90dae49",
        path: "./PlayerCharacterData/Collei/hash.json",
        position_vb: "348e58c4"
    },
    {
        old_draw_vb: "fc54f147",
        new_draw_vb: "75b1de60",
        path: "./PlayerCharacterData/Cyno/hash.json",
        position_vb: "4cc92f60"
    },
    {
        old_draw_vb: "1f2a9e86",
        new_draw_vb: "96cfb1a1",
        path: "./PlayerCharacterData/Dehya/hash.json",
        position_vb: "9aeecbcb"
    },
    {
        old_draw_vb: "56159d74",
        new_draw_vb: "dff0b253",
        path: "./PlayerCharacterData/Diluc/hash.json",
        position_vb: "6fdb0963"
    },
    {
        old_draw_vb: "aeab733d",
        new_draw_vb: "274e5c1a",
        path: "./PlayerCharacterData/DilucFlamme/hash.json",
        position_vb: "a2d909c8"
    },
    {
        old_draw_vb: "3d3acefa",
        new_draw_vb: "b4dfe1dd",
        path: "./PlayerCharacterData/Diona/hash.json",
        position_vb: "e8083f19"
    },
    {
        old_draw_vb: "0f07748b",
        new_draw_vb: "86e25bac",
        path: "./PlayerCharacterData/Dori/hash.json",
        position_vb: "2a2a63ab"
    },
    {
        old_draw_vb: "9bd0821b",
        new_draw_vb: "1235ad3c",
        path: "./PlayerCharacterData/Eula/hash.json",
        position_vb: "107ba6e7"
    },
    {
        old_draw_vb: "5abe4c02",
        new_draw_vb: "d35b6325",
        path: "./PlayerCharacterData/Faruzan/hash.json",
        position_vb: "6162188c"
    },
    {
        old_draw_vb: "6c491d3b",
        new_draw_vb: "e5ac321c",
        path: "./PlayerCharacterData/Fischl/hash.json",
        position_vb: "9838aedf"
    },
    {
        old_draw_vb: "3cc8f82b",
        new_draw_vb: "b52dd70c",
        path: "./PlayerCharacterData/FischlHighness/hash.json",
        position_vb: "8f473224"
    },
    {
        old_draw_vb: "403b6be5",
        new_draw_vb: "7bf55145",
        path: "./PlayerCharacterData/Freminet/hash.json",
        position_vb: "d2bfc751"
    },
    {
        old_draw_vb: "721ca964",
        new_draw_vb: "fbf98643",
        path: "./PlayerCharacterData/Ganyu/hash.json",
        position_vb: "a5169f1d"
    },
    {
        old_draw_vb: "1cbe5f20",
        new_draw_vb: "955b7007",
        path: "./PlayerCharacterData/Gorou/hash.json",
        position_vb: "3ce94cac"
    },
    {
        old_draw_vb: "17f8cba1",
        new_draw_vb: "9e1de486",
        path: "./PlayerCharacterData/Heizou/hash.json",
        position_vb: "51a75ba6"
    },
    {
        old_draw_vb: "60345291",
        new_draw_vb: "e9d17db6",
        path: "./PlayerCharacterData/HuTao/hash.json",
        position_vb: "dd16576c"
    },
    {
        old_draw_vb: "de4c81ac",
        new_draw_vb: "57a9ae8b",
        path: "./PlayerCharacterData/Itto/hash.json",
        position_vb: "3e61a41f"
    },
    {
        old_draw_vb: "e6055135",
        new_draw_vb: "6fe07e12",
        path: "./PlayerCharacterData/Jean/hash.json",
        position_vb: "191af650"
    },
    {
        old_draw_vb: "2a29e333",
        new_draw_vb: "a3cccc14",
        path: "./PlayerCharacterData/JeanCN/hash.json",
        position_vb: "93bb2522"
    },
    {
        old_draw_vb: "972d56ee",
        new_draw_vb: "1ec879c9",
        path: "./PlayerCharacterData/JeanSea/hash.json",
        position_vb: "16fef1eb"
    },
    {
        old_draw_vb: "4b0aa762",
        new_draw_vb: "c2ef8845",
        path: "./PlayerCharacterData/Kaeya/hash.json",
        position_vb: "8a081f34"
    },
    {
        old_draw_vb: "d155c95f",
        new_draw_vb: "58b0e678",
        path: "./PlayerCharacterData/Kazuha/hash.json",
        position_vb: "7c0c47b3"
    },
    {
        old_draw_vb: "4526145e",
        new_draw_vb: "ccc33b79",
        path: "./PlayerCharacterData/Keqing/hash.json",
        position_vb: "3aaf3e94"
    },
    {
        old_draw_vb: "efcc8769",
        new_draw_vb: "6629a84e",
        path: "./PlayerCharacterData/KeqingOpulent/hash.json",
        position_vb: "0d7e3cc5"
    },
    {
        old_draw_vb: "e656b9fd",
        new_draw_vb: "6fb396da",
        path: "./PlayerCharacterData/Kirara/hash.json",
        position_vb: "cc833025"
    },
    {
        old_draw_vb: "52469e36",
        new_draw_vb: "dba3b111",
        path: "./PlayerCharacterData/Klee/hash.json",
        position_vb: "dcd74904"
    },
    {
        old_draw_vb: "ef23d42d",
        new_draw_vb: "66c6fb0a",
        path: "./PlayerCharacterData/Kokomi/hash.json",
        position_vb: "dde4750a"
    },
    {
        old_draw_vb: "4320e589",
        new_draw_vb: "cac5caae",
        path: "./PlayerCharacterData/KujouSara/hash.json",
        position_vb: "b82eaa26"
    },
    {
        old_draw_vb: "ad28f613",
        new_draw_vb: "24cdd934",
        path: "./PlayerCharacterData/Layla/hash.json",
        position_vb: "2656ccca"
    },
    {
        old_draw_vb: "6f4a034a",
        new_draw_vb: "e6af2c6d",
        path: "./PlayerCharacterData/Lisa/hash.json",
        position_vb: "2a557add"
    },
    {
        old_draw_vb: "362fb2b3",
        new_draw_vb: "bfca9d94",
        path: "./PlayerCharacterData/LisaStudent/hash.json",
        position_vb: "37c70461"
    },
    {
        old_draw_vb: "bb7a73b6",
        new_draw_vb: "329f5c91",
        path: "./PlayerCharacterData/Lynette/hash.json",
        position_vb: "98eb2db4"
    },
    {
        old_draw_vb: "14be9ad8",
        new_draw_vb: "9d5bb5ff",
        path: "./PlayerCharacterData/Mika/hash.json",
        position_vb: "1876e82e"
    },
    {
        old_draw_vb: "00741928",
        new_draw_vb: "8991360f",
        path: "./PlayerCharacterData/Mona/hash.json",
        position_vb: "20d0bfab"
    },
    {
        old_draw_vb: "41f18240",
        new_draw_vb: "c814ad67",
        path: "./PlayerCharacterData/MonaCN/hash.json",
        position_vb: "ee5ed1dc"
    },
    {
        old_draw_vb: "67d2cdf0",
        new_draw_vb: "ee37e2d7",
        path: "./PlayerCharacterData/Nahida/hash.json",
        position_vb: "37ef15ec"
    },
    {
        old_draw_vb: "2f95abf6",
        new_draw_vb: "a67084d1",
        path: "./PlayerCharacterData/Nilou/hash.json",
        position_vb: "b2acc1df"
    },
    {
        old_draw_vb: "e4fc5902",
        new_draw_vb: "6d197625",
        path: "./PlayerCharacterData/Ningguang/hash.json",
        position_vb: "55b43e99"
    },
    {
        old_draw_vb: "10de9c78",
        new_draw_vb: "993bb35f",
        path: "./PlayerCharacterData/NingguangOrchid/hash.json",
        position_vb: "db37b198"
    },
    {
        old_draw_vb: "57128011",
        new_draw_vb: "def7af36",
        path: "./PlayerCharacterData/Noelle/hash.json",
        position_vb: "d1384d15"
    },
    {
        old_draw_vb: "1c88b789",
        new_draw_vb: "956d98ae",
        path: "./PlayerCharacterData/Qiqi/hash.json",
        position_vb: "cad5bebb"
    },
    {
        old_draw_vb: "a05e7bec",
        new_draw_vb: "29bb54cb",
        path: "./PlayerCharacterData/RaidenShogun/hash.json",
        position_vb: "e48c61f3"
    },
    {
        old_draw_vb: "3503373e",
        new_draw_vb: "bce61819",
        path: "./PlayerCharacterData/Razor/hash.json",
        position_vb: "a099b935"
    },
    {
        old_draw_vb: "9e1868d9",
        new_draw_vb: "17fd47fe",
        path: "./PlayerCharacterData/Rosaria/hash.json",
        position_vb: "748f40a5"
    },
    {
        old_draw_vb: "f3d4a01a",
        new_draw_vb: "7a318f3d",
        path: "./PlayerCharacterData/RosariaCN/hash.json",
        position_vb: "59a1f8b1"
    },
    {
        old_draw_vb: "f3be854a",
        new_draw_vb: "9e367179",
        path: "./PlayerCharacterData/Sayu/hash.json",
        position_vb: "c70b7fce"
    },
    {
        old_draw_vb: "17d35e5e",
        new_draw_vb: "7a5baa6d",
        path: "./PlayerCharacterData/Sayu/hash.json",
        position_vb: "719e12da"
    },
    {
        old_draw_vb: "fde191d7",
        new_draw_vb: "7404bef0",
        path: "./PlayerCharacterData/Shenhe/hash.json",
        position_vb: "e44b58b5"
    },
    {
        old_draw_vb: "f82b93f8",
        new_draw_vb: "71cebcdf",
        path: "./PlayerCharacterData/Shinobu/hash.json",
        position_vb: "7cfb62ea"
    },
    {
        old_draw_vb: "fc07e82b",
        new_draw_vb: "75e2c70c",
        path: "./PlayerCharacterData/Sucrose/hash.json",
        position_vb: "b655c335"
    },
    {
        old_draw_vb: "33c2fa53",
        new_draw_vb: "ba27d574",
        path: "./PlayerCharacterData/Thoma/hash.json",
        position_vb: "24ecd71a"
    },
    {
        old_draw_vb: "57897152",
        new_draw_vb: "de6c5e75",
        path: "./PlayerCharacterData/Tighnari/hash.json",
        position_vb: "531187c9"
    },
    {
        old_draw_vb: "609d4578",
        new_draw_vb: "e9786a5f",
        path: "./PlayerCharacterData/TravelerBoy/hash.json",
        position_vb: "c77e380b"
    },
    {
        old_draw_vb: "0d8adebb",
        new_draw_vb: "846ff19c",
        path: "./PlayerCharacterData/TravelerGirl/hash.json",
        position_vb: "8239be13"
    },
    {
        old_draw_vb: "5c8b4993",
        new_draw_vb: "d56e66b4",
        path: "./PlayerCharacterData/Venti/hash.json",
        position_vb: "635466ca"
    },
    {
        old_draw_vb: "e9bf37f1",
        new_draw_vb: "605a18d6",
        path: "./PlayerCharacterData/Wanderer/hash.json",
        position_vb: "d74251a0"
    },
    {
        old_draw_vb: "0d628859",
        new_draw_vb: "8487a77e",
        path: "./PlayerCharacterData/Wanderer/hash.json",
        position_vb: "0110e1c7"
    },
    {
        old_draw_vb: "94523bab",
        new_draw_vb: "1db7148c",
        path: "./PlayerCharacterData/Xiangling/hash.json",
        position_vb: "9427917d"
    },
    {
        old_draw_vb: "e6f38080",
        new_draw_vb: "6f16afa7",
        path: "./PlayerCharacterData/Xiao/hash.json",
        position_vb: "7f5ef8cc"
    },
    {
        old_draw_vb: "f9caefa2",
        new_draw_vb: "702fc085",
        path: "./PlayerCharacterData/Xingqiu/hash.json",
        position_vb: "25aed172"
    },
    {
        old_draw_vb: "1a3107d3",
        new_draw_vb: "93d428f4",
        path: "./PlayerCharacterData/Xinyan/hash.json",
        position_vb: "b3d31859"
    },
    {
        old_draw_vb: "7eb264fd",
        new_draw_vb: "f7574bda",
        path: "./PlayerCharacterData/Yae/hash.json",
        position_vb: "3a7f71f5"
    },
    {
        old_draw_vb: "a2305da8",
        new_draw_vb: "2bd5728f",
        path: "./PlayerCharacterData/Yanfei/hash.json",
        position_vb: "eb8b62d3"
    },
    {
        old_draw_vb: "cf68b0bd",
        new_draw_vb: "468d9f9a",
        path: "./PlayerCharacterData/YaoYao/hash.json",
        position_vb: "293449d6"
    },
    {
        old_draw_vb: "589fed34",
        new_draw_vb: "d17ac213",
        path: "./PlayerCharacterData/Yelan/hash.json",
        position_vb: "c58c76f9"
    },
    {
        old_draw_vb: "25fea501",
        new_draw_vb: "ac1b8a26",
        path: "./PlayerCharacterData/Yoimiya/hash.json",
        position_vb: "65618289"
    },
    {
        old_draw_vb: "423e7a1d",
        new_draw_vb: "cbdb553a",
        path: "./PlayerCharacterData/YunJin/hash.json",
        position_vb: "221f052e"
    },
    {
        old_draw_vb: "e646f8d3",
        new_draw_vb: "6fa3d7f4",
        path: "./PlayerCharacterData/Zhongli/hash.json",
        position_vb: "a75ba32e"
    },
    {
        old_draw_vb: "98f4d6a5",
        new_draw_vb: "1111f982",
        path: "./SkillData/CandaceShield/CandaceShield/hash.json",
        position_vb: "6251a4f6"
    },
    {
        old_draw_vb: "794c9a25",
        new_draw_vb: "4f7d845e",
        path: "./SkillData/CynoWolfMask/hash.json",
        position_vb: "c30c8a7c"
    },
    {
        old_draw_vb: "bf67e56a",
        new_draw_vb: "",
        path: "./SkillData/EulaECape/hash.json",
        position_vb: "3cd81031"
    },
    {
        old_draw_vb: "34e18b4f",
        new_draw_vb: "",
        path: "./SkillData/MoraxLapidis/hash.json",
        position_vb: "abfcaf63"
    },
    {
        old_draw_vb: "0c88df31",
        new_draw_vb: "856df016",
        path: "./SkillData/YaoyaoYuegui/YaoyaoYuegui/hash.json",
        position_vb: "b0ec158e"
    },
    {
        old_draw_vb: "5f75ad65",
        new_draw_vb: "",
        path: "./WeaponData/Bows/3Star/EbonyBow/hash.json",
        position_vb: "d4b8d5d9"
    },
    {
        old_draw_vb: "72285884",
        new_draw_vb: "fbcd77a3",
        path: "./WeaponData/Bows/3Star/Messenger/hash.json",
        position_vb: "d8763b05"
    },
    {
        old_draw_vb: "9d71a25c",
        new_draw_vb: "14948d7b",
        path: "./WeaponData/Bows/3Star/RavenBow/hash.json",
        position_vb: "774bce90"
    },
    {
        old_draw_vb: "9d71a25c",
        new_draw_vb: "14948d7b",
        path: "./WeaponData/Bows/3Star/RecurveBow/hash.json",
        position_vb: "774bce90"
    },
    {
        old_draw_vb: "9d71a25c",
        new_draw_vb: "14948d7b",
        path: "./WeaponData/Bows/3Star/SharpshooterOath/hash.json",
        position_vb: "774bce90"
    },
    {
        old_draw_vb: "5f75ad65",
        new_draw_vb: "d6908242",
        path: "./WeaponData/Bows/3Star/Slingshot/hash.json",
        position_vb: "d4b8d5d9"
    },
    {
        old_draw_vb: "3e62e970",
        new_draw_vb: "b787c657",
        path: "./WeaponData/Bows/4Star/AlleyHunter/hash.json",
        position_vb: "4dd715ab"
    },
    {
        old_draw_vb: "9a69cc1f",
        new_draw_vb: "",
        path: "./WeaponData/Bows/4Star/BlackcliffWarbow/hash.json",
        position_vb: "c56f30eb"
    },
    {
        old_draw_vb: "2f496de0",
        new_draw_vb: "2f496de0",
        path: "./WeaponData/Bows/4Star/CompoundBow/CompoundBow/hash.json",
        position_vb: "8aff2a81"
    },
    {
        old_draw_vb: "323c13b2",
        new_draw_vb: "",
        path: "./WeaponData/Bows/4Star/EndOfTheLine/hash.json",
        position_vb: "62f21d9f"
    },
    {
        old_draw_vb: "cc870789",
        new_draw_vb: "456228ae",
        path: "./WeaponData/Bows/4Star/FadingTwilight/hash.json",
        position_vb: "b66b5f0b"
    },
    {
        old_draw_vb: "3e62e970",
        new_draw_vb: "b787c657",
        path: "./WeaponData/Bows/4Star/FavoniusWarbow/hash.json",
        position_vb: "4dd715ab"
    },
    {
        old_draw_vb: "1d041a81",
        new_draw_vb: "94e135a6",
        path: "./WeaponData/Bows/4Star/Hamayumi/hash.json",
        position_vb: "e11f9dc0"
    },
    {
        old_draw_vb: "0af98744",
        new_draw_vb: "831ca863",
        path: "./WeaponData/Bows/4Star/KingSquire/hash.json",
        position_vb: "9432bbb8"
    },
    {
        old_draw_vb: "c5575324",
        new_draw_vb: "4cb27c03",
        path: "./WeaponData/Bows/4Star/MitternachtWaltz/hash.json",
        position_vb: "a6073737"
    },
    {
        old_draw_vb: "be679cbd",
        new_draw_vb: "",
        path: "./WeaponData/Bows/4Star/MouunsMoon/hash.json",
        position_vb: "62b27805"
    },
    {
        old_draw_vb: "23ffa0d4",
        new_draw_vb: "aa1a8ff3",
        path: "./WeaponData/Bows/4Star/Predator/hash.json",
        position_vb: "5b276e9a"
    },
    {
        old_draw_vb: "85f9b4d4",
        new_draw_vb: "0c1c9bf3",
        path: "./WeaponData/Bows/4Star/PrototypeAmber/hash.json",
        position_vb: "7130fdfa"
    },
    {
        old_draw_vb: "47feb048",
        new_draw_vb: "ce1b9f6f",
        path: "./WeaponData/Bows/4Star/RoyalBow/hash.json",
        position_vb: "ecef2c57"
    },
    {
        old_draw_vb: "14b96eda",
        new_draw_vb: "9d5c41fd",
        path: "./WeaponData/Bows/4Star/Rust/hash.json",
        position_vb: "bb654432"
    },
    {
        old_draw_vb: "47feb048",
        new_draw_vb: "ce1b9f6f",
        path: "./WeaponData/Bows/4Star/SacrificialBow/hash.json",
        position_vb: "ecef2c57"
    },
    {
        old_draw_vb: "68722260",
        new_draw_vb: "e1970d47",
        path: "./WeaponData/Bows/4Star/TheStringless/hash.json",
        position_vb: "0d589934"
    },
    {
        old_draw_vb: "3d4c75b4",
        new_draw_vb: "b4a95a93",
        path: "./WeaponData/Bows/4Star/TheViridescentHunt/hash.json",
        position_vb: "9f7a2f44"
    },
    {
        old_draw_vb: "a6ac42c7",
        new_draw_vb: "2f496de0",
        path: "./WeaponData/Bows/4Star/WindblumeOde/hash.json",
        position_vb: "8aff2a81"
    },
    {
        old_draw_vb: "b8cdf6dc",
        new_draw_vb: "3128d9fb",
        path: "./WeaponData/Bows/5Star/AmosBow/hash.json",
        position_vb: "8bf32329"
    },
    {
        old_draw_vb: "2b378d03",
        new_draw_vb: "a2d2a224",
        path: "./WeaponData/Bows/5Star/AquaSimulacra/hash.json",
        position_vb: "7fc55510"
    },
    {
        old_draw_vb: "237ed8c3",
        new_draw_vb: "aa9bf7e4",
        path: "./WeaponData/Bows/5Star/ElegyForTheEnd/hash.json",
        position_vb: "f451d14b"
    },
    {
        old_draw_vb: "13c9cd24",
        new_draw_vb: "",
        path: "./WeaponData/Bows/5Star/HuntersPath/hash.json",
        position_vb: "152282fa"
    },
    {
        old_draw_vb: "53166598",
        new_draw_vb: "daf34abf",
        path: "./WeaponData/Bows/5Star/PolarStar/hash.json",
        position_vb: "791adc59"
    },
    {
        old_draw_vb: "2256affe",
        new_draw_vb: "abb380d9",
        path: "./WeaponData/Bows/5Star/SkywardHarp/hash.json",
        position_vb: "2006ab8b"
    },
    {
        old_draw_vb: "ac5cbd73",
        new_draw_vb: "25b99254",
        path: "./WeaponData/Bows/5Star/ThunderingPulse/hash.json",
        position_vb: "c0afaa8d"
    },
    {
        old_draw_vb: "40cc5918",
        new_draw_vb: "",
        path: "./WeaponData/Catalysts/3Star/AmberBead/hash.json",
        position_vb: "905bd979"
    },
    {
        old_draw_vb: "40cc5918",
        new_draw_vb: "c929763f",
        path: "./WeaponData/Catalysts/3Star/EmeraldOrb/hash.json",
        position_vb: "905bd979"
    },
    {
        old_draw_vb: "e9bccdf0",
        new_draw_vb: "6059e2d7",
        path: "./WeaponData/Catalysts/3Star/MagicGuide/hash.json",
        position_vb: "2275608c"
    },
    {
        old_draw_vb: "e9bccdf0",
        new_draw_vb: "6059e2d7",
        path: "./WeaponData/Catalysts/3Star/OtherworldlyStory/hash.json",
        position_vb: "2275608c"
    },
    {
        old_draw_vb: "e9bccdf0",
        new_draw_vb: "6059e2d7",
        path: "./WeaponData/Catalysts/3Star/ThrillingTalesOfDragonSlayer/hash.json",
        position_vb: "2275608c"
    },
    {
        old_draw_vb: "7aaaeece",
        new_draw_vb: "f34fc1e9",
        path: "./WeaponData/Catalysts/3Star/TwinNephrite/hash.json",
        position_vb: "a15deee9"
    },
    {
        old_draw_vb: "85f9b4d4",
        new_draw_vb: "0c1c9bf3",
        path: "./WeaponData/Catalysts/4Star/BlackcliffAgate/hash.json",
        position_vb: "7130fdfa"
    },
    {
        old_draw_vb: "132ba218",
        new_draw_vb: "9ace8d3f",
        path: "./WeaponData/Catalysts/4Star/DodocoTales/hash.json",
        position_vb: "64b80260"
    },
    {
        old_draw_vb: "de3cdc0c",
        new_draw_vb: "57d9f32b",
        path: "./WeaponData/Catalysts/4Star/EyeOfPerception/hash.json",
        position_vb: "0447a00e"
    },
    {
        old_draw_vb: "7784a0cb",
        new_draw_vb: "fe618fec",
        path: "./WeaponData/Catalysts/4Star/FavoniusCodex/hash.json",
        position_vb: "1578acf2"
    },
    {
        old_draw_vb: "936908a5",
        new_draw_vb: "",
        path: "./WeaponData/Catalysts/4Star/Frostbearer/hash.json",
        position_vb: "166849f8"
    },
    {
        old_draw_vb: "9894ca2b",
        new_draw_vb: "",
        path: "./WeaponData/Catalysts/4Star/FuitOfFultilment/hash.json",
        position_vb: "280cc901"
    },
    {
        old_draw_vb: "c3dde69c",
        new_draw_vb: "4a38c9bb",
        path: "./WeaponData/Catalysts/4Star/HakushinRing/hash.json",
        position_vb: "4ec23e4a"
    },
    {
        old_draw_vb: "132ba218",
        new_draw_vb: "9ace8d3f",
        path: "./WeaponData/Catalysts/4Star/MappaMare/hash.json",
        position_vb: "64b80260"
    },
    {
        old_draw_vb: "3aa3744d",
        new_draw_vb: "f4dd0d2b",
        path: "./WeaponData/Catalysts/4Star/OathswornEye/hash.json",
        position_vb: "392fd268"
    },
    {
        old_draw_vb: "85f9b4d4",
        new_draw_vb: "0c1c9bf3",
        path: "./WeaponData/Catalysts/4Star/PrototypeAmber/hash.json",
        position_vb: "7130fdfa"
    },
    {
        old_draw_vb: "02f77561",
        new_draw_vb: "",
        path: "./WeaponData/Catalysts/4Star/RoyalGrimoire/hash.json",
        position_vb: "f3da8c9e"
    },
    {
        old_draw_vb: "02f77561",
        new_draw_vb: "8b125a46",
        path: "./WeaponData/Catalysts/4Star/SacrificialFragments/hash.json",
        position_vb: "f3da8c9e"
    },
    {
        old_draw_vb: "297e102c",
        new_draw_vb: "5d1cef93",
        path: "./WeaponData/Catalysts/4Star/SolarPearl/hash.json",
        position_vb: "373580ad"
    },
    {
        old_draw_vb: "368b9922",
        new_draw_vb: "911ff708",
        path: "./WeaponData/Catalysts/4Star/WanderingEvenstar/hash.json",
        position_vb: "03f91f7d"
    },
    {
        old_draw_vb: "0473f704",
        new_draw_vb: "8d96d823",
        path: "./WeaponData/Catalysts/4Star/Widsith/hash.json",
        position_vb: "8dc249b6"
    },
    {
        old_draw_vb: "7784a0cb",
        new_draw_vb: "fe618fec",
        path: "./WeaponData/Catalysts/4Star/WineAndSong/hash.json",
        position_vb: "1578acf2"
    },
    {
        old_draw_vb: "6b9a6156",
        new_draw_vb: "e27f4e71",
        path: "./WeaponData/Catalysts/5Star/aThousandFloatingDreams/hash.json",
        position_vb: "1ec239cd"
    },
    {
        old_draw_vb: "3fc5d187",
        new_draw_vb: "b620fea0",
        path: "./WeaponData/Catalysts/5Star/EverlastingMoonglow/hash.json",
        position_vb: "03a34660"
    },
    {
        old_draw_vb: "d45c086d",
        new_draw_vb: "",
        path: "./WeaponData/Catalysts/5Star/JadefallsSplendor/hash.json",
        position_vb: "d45c086d"
    },
    {
        old_draw_vb: "072cd528",
        new_draw_vb: "b620fea0",
        path: "./WeaponData/Catalysts/5Star/KagurasVerity/hash.json",
        position_vb: "c33f468a"
    },
    {
        old_draw_vb: "ec6497b8",
        new_draw_vb: "6581b89f",
        path: "./WeaponData/Catalysts/5Star/LostPrayer/hash.json",
        position_vb: "a1c0ac1a"
    },
    {
        old_draw_vb: "339820e0",
        new_draw_vb: "",
        path: "./WeaponData/Catalysts/5Star/MemoryOfDust/hash.json",
        position_vb: "23e8426e"
    },
    {
        old_draw_vb: "29af7af2",
        new_draw_vb: "",
        path: "./WeaponData/Catalysts/5Star/SkywardAtlas/hash.json",
        position_vb: "5d14263f"
    },
    {
        old_draw_vb: "ae8ddaba",
        new_draw_vb: "2768f59d",
        path: "./WeaponData/Catalysts/5Star/Tullaytullah'sRemembrance/hash.json",
        position_vb: "c9a25c6f"
    },
    {
        old_draw_vb: "f0f0f4a4",
        new_draw_vb: "7915db83",
        path: "./WeaponData/Claymores/4Star/BlackcliffSlasher/hash.json",
        position_vb: "1d87d02e"
    },
    {
        old_draw_vb: "24a68f9c",
        new_draw_vb: "",
        path: "./WeaponData/Claymores/4Star/Katsuragikiri/hash.json",
        position_vb: "099a58eb"
    },
    {
        old_draw_vb: "f0f0f4a4",
        new_draw_vb: "7915db83",
        path: "./WeaponData/Claymores/4Star/PrototypeArchaic/hash.json",
        position_vb: "1d87d02e"
    },
    {
        old_draw_vb: "9c289a2d",
        new_draw_vb: "15cdb50a",
        path: "./WeaponData/Enemies/HilicurlRangerHydroScythe/hash.json",
        position_vb: "c3c9ccbc"
    },
    {
        old_draw_vb: "ddb5e853",
        new_draw_vb: "f92902c2",
        path: "./WeaponData/Enemies/KairagiFierySword/hash.json",
        position_vb: "ddb5e853"
    },
    {
        old_draw_vb: "7c3a09f3",
        new_draw_vb: "12221810",
        path: "./WeaponData/Enemies/KairagiThunderSword/hash.json",
        position_vb: "7c3a09f3"
    },
    {
        old_draw_vb: "98b74969",
        new_draw_vb: "78f25ccd",
        path: "./WeaponData/Enemies/LineBreaker/hash.json",
        position_vb: "8f941cb5"
    },
    {
        old_draw_vb: "8673d264",
        new_draw_vb: "",
        path: "./WeaponData/Enemies/MitaAxe/hash.json",
        position_vb: "85b5f67d"
    },
    {
        old_draw_vb: "de8daa01",
        new_draw_vb: "",
        path: "./WeaponData/Enemies/RagingTideHarbingers/hash.json",
        position_vb: "a471337a"
    },
    {
        old_draw_vb: "da4eed2c",
        new_draw_vb: "fd5a432d",
        path: "./WeaponData/Enemies/StandardBearer/hash.json",
        position_vb: "2cf38039"
    },
    {
        old_draw_vb: "29ea51e2",
        new_draw_vb: "bf638a7d",
        path: "./WeaponData/Enemies/Windcutter/hash.json",
        position_vb: "6767ab1d"
    },
    {
        old_draw_vb: "f4a61aad",
        new_draw_vb: "7d43358a",
        path: "./WeaponData/Polearms/4Star/PrototypeStarglitter/hash.json",
        position_vb: "3c37e40c"
    },
    {
        old_draw_vb: "e30ca5e9",
        new_draw_vb: "",
        path: "./WeaponData/Polearms/NonPlayer/MoraxVortex/hash.json",
        position_vb: "1fa29595"
    },
    {
        old_draw_vb: "f35127aa",
        new_draw_vb: "7ab4088d",
        path: "./WeaponData/Swords/4Star/AmenomaKageuchi/hash.json",
        position_vb: "5e0231af"
    },
    {
        old_draw_vb: "bffaf2c7",
        new_draw_vb: "",
        path: "./WeaponData/Swords/4Star/BlackcliffLongsword/hash.json",
        position_vb: "18802bf2"
    },
    {
        old_draw_vb: "bffaf2c7",
        new_draw_vb: "361fdde0",
        path: "./WeaponData/Swords/4Star/PrototypeRancour/hash.json",
        position_vb: "18802bf2"
    },
    {
        old_draw_vb: "6c96b19c",
        new_draw_vb: "",
        path: "./WeaponData/Swords/5Star/HaranGeppaku/hash.json",
        position_vb: "a07c44cc"
    },
    {
        old_draw_vb: "c51d9d82",
        new_draw_vb: "",
        path: "./WeaponData/Swords/NonPlayer/AetherSword/hash.json",
        position_vb: "ad72f59c"
    },
    {
        old_draw_vb: "81eb3ae6",
        new_draw_vb: "",
        path: "./WeaponData/Swords/NonPlayer/LumineSword/hash.json",
        position_vb: "2d278663"
    },
    {
        old_draw_vb: "c605bcd4",
        new_draw_vb: "4fe093f3",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "58f0252f",
        new_draw_vb: "d1150a08",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "02bcc081",
        new_draw_vb: "8b59efa6",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "f4abd771",
        new_draw_vb: "7d4ef856",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "5b28685e",
        new_draw_vb: "d2cd4779",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "9810aed3",
        new_draw_vb: "",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "ee618640",
        new_draw_vb: "6784a967",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "0d77d598",
        new_draw_vb: "8492fabf",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "ec14ca18",
        new_draw_vb: "65f1e53f",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "e19b42c7",
        new_draw_vb: "687e6de0",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "9c51f5f6",
        new_draw_vb: "15b4dad1",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "83589493",
        new_draw_vb: "0abdbbb4",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "6926d7cd",
        new_draw_vb: "e0c3f8ea",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "c2702a80",
        new_draw_vb: "4b9505a7",
        path: "",
        position_vb: ""
    },
    {
        old_draw_vb: "e96f4fc3",
        new_draw_vb: "608a60e4",
        path: "./PlayerCharacterData/Kaveh/hash.json",
        position_vb: "b56fd424"
    },
    {
        old_draw_vb: "224f2d0f",
        new_draw_vb: "abaa0228",
        path: "./SkillData/LyneyHat/hash.json",
        position_vb: "5caaa94b"
    },
    {
        old_draw_vb: "f37168c3",
        new_draw_vb: "7a9447e4",
        path: "./PlayerCharacterData/Lyney/hash.json",
        position_vb: "6f7b7740"
    },
    {
        old_draw_vb: "6234ae22",
        new_draw_vb: "ebd18105",
        path: "./PlayerCharacterData/KleeBlossomingStarlight/hash.json",
        position_vb: "0f5fedb4"
    },
    {
        old_draw_vb: "26ad581d",
        new_draw_vb: "af48773a",
        path: "./SkillData/CatBox/hash.json",
        position_vb: "d50d0b45"
    },
    {
        old_draw_vb: "0525ea44",
        new_draw_vb: "d17ac213",
        path: "./PlayerCharacterData/Yelan/hash.json",
        position_vb: ""
    },
];

// MARK: IB HASHES
const alljson_43: JsonData43[] = [
    {
        old_ib: "20f5e761",
        new_ib: "",
        path: "./EnemyData/AbyssHerald/hash.json"
    },
    {
        old_ib: "a3dd8fae",
        new_ib: "",
        path: "./EnemyData/AbyssHerald/hash.json"
    },
    {
        old_ib: "cd59deec",
        new_ib: "",
        path: "./EnemyData/AbyssLectorElectro/hash.json"
    },
    {
        old_ib: "16e5380a",
        new_ib: "",
        path: "./EnemyData/AbyssLectorElectro/hash.json"
    },
    {
        old_ib: "3081295f",
        new_ib: "",
        path: "./EnemyData/AbyssLectorPyro/hash.json"
    },
    {
        old_ib: "3e3f6114",
        new_ib: "",
        path: "./EnemyData/AbyssLectorPyro/hash.json"
    },
    {
        old_ib: "4f5a6086",
        new_ib: "",
        path: "./EnemyData/Andrius/hash.json"
    },
    {
        old_ib: "32c81efa",
        new_ib: "",
        path: "./EnemyData/Andrius/hash.json"
    },
    {
        old_ib: "f7b61507",
        new_ib: "cf627fd8",
        path: "./EnemyData/AnnihilationSpecialistMek/hash.json"
    },
    {
        old_ib: "a0057feb",
        new_ib: "",
        path: "./EnemyData/Azhdaha/hash.json"
    },
    {
        old_ib: "81b66b25",
        new_ib: "",
        path: "./EnemyData/Azhdaha/hash.json"
    },
    {
        old_ib: "5f4f7a04",
        new_ib: "",
        path: "./EnemyData/BigSlimeDendro/hash.json"
    },
    {
        old_ib: "5d4bd489",
        new_ib: "",
        path: "./EnemyData/BigSlimeDendro/hash.json"
    },
    {
        old_ib: "aed62f40",
        new_ib: "",
        path: "./EnemyData/BigSlimeDendro/hash.json"
    },
    {
        old_ib: "7c1a4d41",
        new_ib: "",
        path: "./EnemyData/BlackSerpentKnightRockBreakerAx/hash.json"
    },
    {
        old_ib: "7fd2dc62",
        new_ib: "",
        path: "./EnemyData/BlackSerpentWindcutter/hash.json"
    },
    {
        old_ib: "3e69e391",
        new_ib: "06bd894e",
        path: "./EnemyData/CryoCicinMage/hash.json"
    },
    {
        old_ib: "5ae75c8d",
        new_ib: "",
        path: "./EnemyData/CryoHypostasis/hash.json"
    },
    {
        old_ib: "4fd8fdd5",
        new_ib: "",
        path: "./EnemyData/CryoHypostasis/hash.json"
    },
    {
        old_ib: "11247279",
        new_ib: "",
        path: "./EnemyData/Dvalin/hash.json"
    },
    {
        old_ib: "430c9cbe",
        new_ib: "",
        path: "./EnemyData/Dvalin/hash.json"
    },
    {
        old_ib: "4147f32b",
        new_ib: "",
        path: "./EnemyData/ElectroHypostasis/hash.json"
    },
    {
        old_ib: "3dbb0e08",
        new_ib: "",
        path: "./EnemyData/ElectroHypostasis/hash.json"
    },
    {
        old_ib: "11c94b2a",
        new_ib: "",
        path: "./EnemyData/ElectroRegisvine/hash.json"
    },
    {
        old_ib: "704e68cf",
        new_ib: "",
        path: "./EnemyData/ElectroRegisvine/hash.json"
    },
    {
        old_ib: "637cda49",
        new_ib: "",
        path: "./EnemyData/ElectroRegisvine/hash.json"
    },
    {
        old_ib: "6788cef6",
        new_ib: "",
        path: "./EnemyData/ElectroRegisvine/hash.json"
    },
    {
        old_ib: "0cb832e3",
        new_ib: "",
        path: "./EnemyData/ElectroRegisvine/hash.json"
    },
    {
        old_ib: "c24fce2a",
        new_ib: "",
        path: "./EnemyData/ElectroRegisvine/hash.json"
    },
    {
        old_ib: "b3e53e87",
        new_ib: "",
        path: "./EnemyData/ElectroRegisvine/hash.json"
    },
    {
        old_ib: "be132b2e",
        new_ib: "86c741f1",
        path: "./EnemyData/EremiteFloralRingDancer/hash.json"
    },
    {
        old_ib: "805be8c0",
        new_ib: "",
        path: "./EnemyData/FatuiAgent/hash.json"
    },
    {
        old_ib: "049a02cd",
        new_ib: "3c4e6812",
        path: "./EnemyData/FatuiElectroCicinMage/hash.json"
    },
    {
        old_ib: "82d9825b",
        new_ib: "06bd894e",
        path: "./EnemyData/FatuiElectroCicinMage/hash.json"
    },
    {
        old_ib: "7c02f760",
        new_ib: "44d69dbf",
        path: "./EnemyData/FatuiMirrorMaiden/hash.json"
    },
    {
        old_ib: "c943e59e",
        new_ib: "f1978f41",
        path: "./EnemyData/Hilichurl/hash.json"
    },
    {
        old_ib: "f1cbc62b",
        new_ib: "",
        path: "./EnemyData/HilicurlRangerHydro/hash.json"
    },
    {
        old_ib: "2383d1ff",
        new_ib: "",
        path: "./EnemyData/KairagiFiery/hash.json"
    },
    {
        old_ib: "f80f3489",
        new_ib: "",
        path: "./EnemyData/KairagiThunder/hash.json"
    },
    {
        old_ib: "71c4f64a",
        new_ib: "",
        path: "./EnemyData/LaSignoraHarbingers/hash.json"
    },
    {
        old_ib: "0dcd3f88",
        new_ib: "",
        path: "./EnemyData/LaSignoraHarbingers/hash.json"
    },
    {
        old_ib: "98f98bf3",
        new_ib: "a02de12c",
        path: "./EnemyData/LawachurlGeo/hash.json"
    },
    {
        old_ib: "e7247034",
        new_ib: "",
        path: "./EnemyData/MagatsuMitakeNarukaminoMikoto/hash.json"
    },
    {
        old_ib: "7274a9ef",
        new_ib: "",
        path: "./EnemyData/MagatsuMitakeNarukaminoMikoto/hash.json"
    },
    {
        old_ib: "a191e7c2",
        new_ib: "",
        path: "./EnemyData/MagatsuMitakeNarukaminoMikoto/hash.json"
    },
    {
        old_ib: "7274e051",
        new_ib: "4aa08a8e",
        path: "./EnemyData/Mitachurl/hash.json"
    },
    {
        old_ib: "0b983685",
        new_ib: "334c5c5a",
        path: "./EnemyData/RuinGuard/hash.json"
    },
    {
        old_ib: "8b79cbfa",
        new_ib: "",
        path: "./EnemyData/RuinHunter/hash.json"
    },
    {
        old_ib: "4258388c",
        new_ib: "",
        path: "./EnemyData/TartagliaDelusion/hash.json"
    },
    {
        old_ib: "45404603",
        new_ib: "",
        path: "./EnemyData/TartagliaHarbingers/hash.json"
    },
    {
        old_ib: "400d2ea4",
        new_ib: "",
        path: "./MiscellaneousData/AranaraFlowerCrown/hash.json"
    },
    {
        old_ib: "090cc533",
        new_ib: "",
        path: "./MiscellaneousData/BlackCat/hash.json"
    },
    {
        old_ib: "5fdf1ade",
        new_ib: "",
        path: "./MiscellaneousData/Celestia/hash.json"
    },
    {
        old_ib: "67f708c4",
        new_ib: "",
        path: "./MiscellaneousData/CommonChest/hash.json"
    },
    {
        old_ib: "f591915a",
        new_ib: "cd45fb85",
        path: "./MiscellaneousData/CryoCicinMageLatern/hash.json"
    },
    {
        old_ib: "c2d0f4c4",
        new_ib: "",
        path: "./MiscellaneousData/ExquisiteChest/hash.json"
    },
    {
        old_ib: "e0db0c15",
        new_ib: "",
        path: "./MiscellaneousData/GnosisAnemo/hash.json"
    },
    {
        old_ib: "4f52d09c",
        new_ib: "",
        path: "./MiscellaneousData/GnosisElectro/hash.json"
    },
    {
        old_ib: "152893c9",
        new_ib: "",
        path: "./MiscellaneousData/GnosisGeo/hash.json"
    },
    {
        old_ib: "cb7ed57d",
        new_ib: "",
        path: "./MiscellaneousData/Keqing Ultimate/Kek1/hash.json"
    },
    {
        old_ib: "2d5215f4",
        new_ib: "",
        path: "./MiscellaneousData/Keqing Ultimate/Kek2/hash.json"
    },
    {
        old_ib: "0ee5b35b",
        new_ib: "",
        path: "./MiscellaneousData/Keqing Ultimate/Kek3/hash.json"
    },
    {
        old_ib: "e2a05abd",
        new_ib: "",
        path: "./MiscellaneousData/Keqing Ultimate/Kek4/hash.json"
    },
    {
        old_ib: "e2a05abd",
        new_ib: "",
        path: "./MiscellaneousData/Keqing Ultimate/Kek5/hash.json"
    },
    {
        old_ib: "7628b8f0",
        new_ib: "",
        path: "./MiscellaneousData/Keqing Ultimate/KekHead1/hash.json"
    },
    {
        old_ib: "dd654e81",
        new_ib: "",
        path: "./MiscellaneousData/Keqing Ultimate/KekHead2/hash.json"
    },
    {
        old_ib: "fc423b57",
        new_ib: "",
        path: "./MiscellaneousData/Keqing Ultimate/KekHead3/hash.json"
    },
    {
        old_ib: "fc423b57",
        new_ib: "",
        path: "./MiscellaneousData/Keqing Ultimate/KekHead4/hash.json"
    },
    {
        old_ib: "fc423b57",
        new_ib: "",
        path: "./MiscellaneousData/Keqing Ultimate/KekHead5/hash.json"
    },
    {
        old_ib: "273c64b3",
        new_ib: "",
        path: "./MiscellaneousData/Keqing Ultimate/KekSwords/hash.json"
    },
    {
        old_ib: "1680c178",
        new_ib: "",
        path: "./MiscellaneousData/KeqScroll/hash.json"
    },
    {
        old_ib: "5da3881a",
        new_ib: "",
        path: "./MiscellaneousData/KokoBook/hash.json"
    },
    {
        old_ib: "34ae0d49",
        new_ib: "0c7a6796",
        path: "./MiscellaneousData/Liloupar/hash.json"
    },
    {
        old_ib: "a5253468",
        new_ib: "",
        path: "./MiscellaneousData/LisaStudentBook/hash.json"
    },
    {
        old_ib: "c6a138bd",
        new_ib: "",
        path: "./MiscellaneousData/LuxoriousChest/hash.json"
    },
    {
        old_ib: "92e3f8d8",
        new_ib: "",
        path: "./MiscellaneousData/Moon/hash.json"
    },
    {
        old_ib: "e1b9ebe8",
        new_ib: "",
        path: "./MiscellaneousData/SacredSakura/hash.json"
    },
    {
        old_ib: "ea11a2e3",
        new_ib: "",
        path: "./MiscellaneousData/SacredSakura/hash.json"
    },
    {
        old_ib: "4bbc25d0",
        new_ib: "",
        path: "./MiscellaneousData/SaraWings/hash.json"
    },
    {
        old_ib: "354ad70a",
        new_ib: "0d9ebdd5",
        path: "./MiscellaneousData/ShenheGhost/hash.json"
    },
    {
        old_ib: "150145f6",
        new_ib: "",
        path: "./MiscellaneousData/SignoraWhip/hash.json"
    },
    {
        old_ib: "1e9f4d6e",
        new_ib: "",
        path: "./MiscellaneousData/TheSevenBarbatos/hash.json"
    },
    {
        old_ib: "6480d788",
        new_ib: "",
        path: "./MiscellaneousData/TheSevenBarbatos/hash.json"
    },
    {
        old_ib: "2ed0da97",
        new_ib: "",
        path: "./MiscellaneousData/TheSevenBarbatos/hash.json"
    },
    {
        old_ib: "c2da0e78",
        new_ib: "",
        path: "./MiscellaneousData/TheSevenBeelzebul/hash.json"
    },
    {
        old_ib: "95ba3869",
        new_ib: "",
        path: "./MiscellaneousData/TheSevenKusanali/hash.json"
    },
    {
        old_ib: "b4cf8a79",
        new_ib: "",
        path: "./MiscellaneousData/TheSevenMorax/hash.json"
    },
    {
        old_ib: "6480d788",
        new_ib: "",
        path: "./MiscellaneousData/TheSevenPart/hash.json"
    },
    {
        old_ib: "2ed0da97",
        new_ib: "",
        path: "./MiscellaneousData/TheSevenPart/hash.json"
    },
    {
        old_ib: "e3773cbf",
        new_ib: "",
        path: "./MiscellaneousData/VentiStatue/hash.json"
    },
    {
        old_ib: "ddcd0f8b",
        new_ib: "e5196554",
        path: "./MiscellaneousData/Waverider/hash.json"
    },
    {
        old_ib: "a5253468",
        new_ib: "",
        path: "./MiscellaneousData/XingBook/hash.json"
    },
    {
        old_ib: "dfc55c9c",
        new_ib: "",
        path: "./MiscellaneousData/YaeMiko5Tails/hash.json"
    },
    {
        old_ib: "dabb96e5",
        new_ib: "",
        path: "./NPCData/Asmoday/hash.json"
    },
    {
        old_ib: "14b2e312",
        new_ib: "2c6689cd",
        path: "./NPCData/BaronBunny/hash.json"
    },
    {
        old_ib: "981d8cba",
        new_ib: "a0c9e665",
        path: "./NPCData/Chubby/hash.json"
    },
    {
        old_ib: "bedc98e1",
        new_ib: "",
        path: "./NPCData/Crab/Crab/hash.json"
    },
    {
        old_ib: "f7a7558a",
        new_ib: "",
        path: "./NPCData/Dainsleif/hash.json"
    },
    {
        old_ib: "f4e8e4b1",
        new_ib: "",
        path: "./NPCData/DainsleifFace/hash.json"
    },
    {
        old_ib: "80889fe9",
        new_ib: "",
        path: "./NPCData/DainsleifFace/hash.json"
    },
    {
        old_ib: "718d6b95",
        new_ib: "",
        path: "./NPCData/DainsleifFace/hash.json"
    },
    {
        old_ib: "ce43a52e",
        new_ib: "",
        path: "./NPCData/Dottore/hash.json"
    },
    {
        old_ib: "669be7b7",
        new_ib: "5e4f8d68",
        path: "./NPCData/Furina(NPC) pre4-2/hash.json"
    },
    {
        old_ib: "eea6a2fa",
        new_ib: "045e580b",
        path: "./NPCData/Furina(NPC) pre4-2/hash.json"
    },
    {
        old_ib: "c88c3d6e",
        new_ib: "f05857b1",
        path: "./NPCData/Guoba/hash.json"
    },
    {
        old_ib: "64bf7704",
        new_ib: "5c6b1ddb",
        path: "./NPCData/Jeht/hash.json"
    },
    {
        old_ib: "c5c4cc9f",
        new_ib: "fd10a640",
        path: "./NPCData/Katheryne/hash.json"
    },
    {
        old_ib: "80106aff",
        new_ib: "80106aff",
        path: "./NPCData/Katheryne/hash.json"
    },
    {
        old_ib: "cb9323d1",
        new_ib: "fd10a640",
        path: "./NPCData/Katheryne/hash.json"
    },
    {
        old_ib: "83f11108",
        new_ib: "",
        path: "./NPCData/LaSignora/hash.json"
    },
    {
        old_ib: "b2c5a4e3",
        new_ib: "",
        path: "./NPCData/LaSignoraFace/hash.json"
    },
    {
        old_ib: "04814b8d",
        new_ib: "",
        path: "./NPCData/LaSignoraFace/hash.json"
    },
    {
        old_ib: "c28360fe",
        new_ib: "",
        path: "./NPCData/LaSignoraFace/hash.json"
    },
    {
        old_ib: "4103ec7e",
        new_ib: "",
        path: "./NPCData/LoliNahida/hash.json"
    },
    {
        old_ib: "3f6d614c",
        new_ib: "07b90b93",
        path: "./NPCData/Octobaby/hash.json"
    },
    {
        old_ib: "3633dea5",
        new_ib: "0ee7b47a",
        path: "./NPCData/Octobaby/hash.json"
    },
    {
        old_ib: "61ab7805",
        new_ib: "597f12da",
        path: "./NPCData/Oz/hash.json"
    },
    {
        old_ib: "6398b643",
        new_ib: "5b4cdc9c",
        path: "./NPCData/Paimon/hash.json"
    },
    {
        old_ib: "f218fa82",
        new_ib: "cacc905d",
        path: "./NPCData/Paimon/hash.json"
    },
    {
        old_ib: "09606e51",
        new_ib: "",
        path: "./NPCData/PaimonFace/hash.json"
    },
    {
        old_ib: "64579783",
        new_ib: "",
        path: "./NPCData/RexLapis/hash.json"
    },
    {
        old_ib: "a1a35456",
        new_ib: "",
        path: "./NPCData/Scaramouche/hash.json"
    },
    {
        old_ib: "b6f37478",
        new_ib: "",
        path: "./NPCData/Scaramouche/hash.json"
    },
    {
        old_ib: "6a2142f1",
        new_ib: "",
        path: "./NPCData/ScaramoucheFace/hash.json"
    },
    {
        old_ib: "695dcf0d",
        new_ib: "",
        path: "./NPCData/ScaramoucheFace/hash.json"
    },
    {
        old_ib: "1ab70582",
        new_ib: "",
        path: "./NPCData/ScaramoucheFace/hash.json"
    },
    {
        old_ib: "896e8a12",
        new_ib: "",
        path: "./NPCData/Ushi/hash.json"
    },
    {
        old_ib: "35a9a3e9",
        new_ib: "0d7dc936",
        path: "./PlayerCharacterData/Albedo/hash.json"
    },
    {
        old_ib: "5b497567",
        new_ib: "639d1fb8",
        path: "./PlayerCharacterData/Alhaitham/hash.json"
    },
    {
        old_ib: "65c9cdc8",
        new_ib: "5d1da717",
        path: "./PlayerCharacterData/Aloy/hash.json"
    },
    {
        old_ib: "9976d124",
        new_ib: "a1a2bbfb",
        path: "./PlayerCharacterData/Amber/hash.json"
    },
    {
        old_ib: "8cc9274b",
        new_ib: "b41d4d94",
        path: "./PlayerCharacterData/AmberCN/hash.json"
    },
    {
        old_ib: "70db78b8",
        new_ib: "",
        path: "./PlayerCharacterData/Arlecchino/hash.json"
    },
    {
        old_ib: "347bb8f8",
        new_ib: "0cafd227",
        path: "./PlayerCharacterData/Ayaka/hash.json"
    },
    {
        old_ib: "dd4f6309",
        new_ib: "e59b09d6",
        path: "./PlayerCharacterData/Ayato/hash.json"
    },
    {
        old_ib: "86df8dd8",
        new_ib: "be0be707",
        path: "./PlayerCharacterData/Baizhu/hash.json"
    },
    {
        old_ib: "231723d2",
        new_ib: "1bc3490d",
        path: "./PlayerCharacterData/Barbara/hash.json"
    },
    {
        old_ib: "a411cfbc",
        new_ib: "9cc5a563",
        path: "./PlayerCharacterData/BarbaraSummertime/hash.json"
    },
    {
        old_ib: "c6004130",
        new_ib: "fed42bef",
        path: "./PlayerCharacterData/Beidou/hash.json"
    },
    {
        old_ib: "f51209fc",
        new_ib: "cdc66323",
        path: "./PlayerCharacterData/Bennett/hash.json"
    },
    {
        old_ib: "9098a3ef",
        new_ib: "a84cc930",
        path: "./PlayerCharacterData/Candace/hash.json"
    },
    {
        old_ib: "c7812015",
        new_ib: "ff554aca",
        path: "./PlayerCharacterData/Charlotte/hash.json"
    },
    {
        old_ib: "e6633da7",
        new_ib: "",
        path: "./PlayerCharacterData/Charlotte/hash.json"
    },
    {
        old_ib: "6bc9f987",
        new_ib: "531d9358",
        path: "./PlayerCharacterData/Childe/hash.json"
    },
    {
        old_ib: "ef095180",
        new_ib: "d7dd3b5f",
        path: "./PlayerCharacterData/ChildeScarf/hash.json"
    },
    {
        old_ib: "34b9b809",
        new_ib: "0c6dd2d6",
        path: "./PlayerCharacterData/Chongyun/hash.json"
    },
    {
        old_ib: "44bb03ab",
        new_ib: "",
        path: "./PlayerCharacterData/Clorinde/hash.json"
    },
    {
        old_ib: "05729218",
        new_ib: "3da6f8c7",
        path: "./PlayerCharacterData/Collei/hash.json"
    },
    {
        old_ib: "97cc2eae",
        new_ib: "af184471",
        path: "./PlayerCharacterData/Cyno/hash.json"
    },
    {
        old_ib: "5b378f51",
        new_ib: "63e3e58e",
        path: "./PlayerCharacterData/Dehya/hash.json"
    },
    {
        old_ib: "d1ac0687",
        new_ib: "e9786c58",
        path: "./PlayerCharacterData/Diluc/hash.json"
    },
    {
        old_ib: "9de6528c",
        new_ib: "a5323853",
        path: "./PlayerCharacterData/DilucFlamme/hash.json"
    },
    {
        old_ib: "4cde183c",
        new_ib: "740a72e3",
        path: "./PlayerCharacterData/Diona/hash.json"
    },
    {
        old_ib: "3c46fe49",
        new_ib: "04929496",
        path: "./PlayerCharacterData/Dori/hash.json"
    },
    {
        old_ib: "5ed7f30e",
        new_ib: "660399d1",
        path: "./PlayerCharacterData/Eula/hash.json"
    },
    {
        old_ib: "c2795dff",
        new_ib: "faad3720",
        path: "./PlayerCharacterData/Faruzan/hash.json"
    },
    {
        old_ib: "5cfc7a92",
        new_ib: "6428104d",
        path: "./PlayerCharacterData/Fischl/hash.json"
    },
    {
        old_ib: "95bf8d7e",
        new_ib: "ad6be7a1",
        path: "./PlayerCharacterData/FischlHighness/hash.json"
    },
    {
        old_ib: "1812a5f8",
        new_ib: "",
        path: "./PlayerCharacterData/Freminet/hash.json"
    },
    {
        old_ib: "3c8a32d4",
        new_ib: "045e580b",
        path: "./PlayerCharacterData/Furina/hash.json"
    },
    {
        old_ib: "669be7b7",
        new_ib: "5e4f8d68",
        path: "./PlayerCharacterData/Furina/hash.json"
    },
    {
        old_ib: "2da186bc",
        new_ib: "1575ec63",
        path: "./PlayerCharacterData/Ganyu/hash.json"
    },
    {
        old_ib: "8a31165b",
        new_ib: "b2e57c84",
        path: "./PlayerCharacterData/Gorou/hash.json"
    },
    {
        old_ib: "ec1dd06b",
        new_ib: "d4c9bab4",
        path: "./PlayerCharacterData/Heizou/hash.json"
    },
    {
        old_ib: "0535853d",
        new_ib: "3de1efe2",
        path: "./PlayerCharacterData/HuTao/hash.json"
    },
    {
        old_ib: "868d1bc7",
        new_ib: "be597118",
        path: "./PlayerCharacterData/Itto/hash.json"
    },
    {
        old_ib: "29835d20",
        new_ib: "115737ff",
        path: "./PlayerCharacterData/Jean/hash.json"
    },
    {
        old_ib: "920c0b3f",
        new_ib: "aad861e0",
        path: "./PlayerCharacterData/JeanCN/hash.json"
    },
    {
        old_ib: "5114a891",
        new_ib: "69c0c24e",
        path: "./PlayerCharacterData/JeanSea/hash.json"
    },
    {
        old_ib: "13eb3d85",
        new_ib: "2b3f575a",
        path: "./PlayerCharacterData/Kaeya/hash.json"
    },
    {
        old_ib: "61b2cce0",
        new_ib: "5966a63f",
        path: "./PlayerCharacterData/Kaveh/hash.json"
    },
    {
        old_ib: "0db8b101",
        new_ib: "356cdbde",
        path: "./PlayerCharacterData/Kazuha/hash.json"
    },
    {
        old_ib: "f325e394",
        new_ib: "cbf1894b",
        path: "./PlayerCharacterData/Keqing/hash.json"
    },
    {
        old_ib: "44bba21c",
        new_ib: "7c6fc8c3",
        path: "./PlayerCharacterData/KeqingOpulent/hash.json"
    },
    {
        old_ib: "ce3dc5a2",
        new_ib: "f6e9af7d",
        path: "./PlayerCharacterData/Kirara/hash.json"
    },
    {
        old_ib: "3fe81b2a",
        new_ib: "073c71f5",
        path: "./PlayerCharacterData/Klee/hash.json"
    },
    {
        old_ib: "742c4ed5",
        new_ib: "4cf8240a",
        path: "./PlayerCharacterData/KleeBlossomingStarlight/hash.json"
    },
    {
        old_ib: "4c44665e",
        new_ib: "74900c81",
        path: "./PlayerCharacterData/Kokomi/hash.json"
    },
    {
        old_ib: "284f55b3",
        new_ib: "109b3f6c",
        path: "./PlayerCharacterData/KujouSara/hash.json"
    },
    {
        old_ib: "b617aa07",
        new_ib: "8ec3c0d8",
        path: "./PlayerCharacterData/Layla/hash.json"
    },
    {
        old_ib: "695e029f",
        new_ib: "518a6840",
        path: "./PlayerCharacterData/Lisa/hash.json"
    },
    {
        old_ib: "cbda8639",
        new_ib: "f30eece6",
        path: "./PlayerCharacterData/LisaStudent/hash.json"
    },
    {
        old_ib: "010cf88a",
        new_ib: "39d89255",
        path: "./PlayerCharacterData/Lynette/hash.json"
    },
    {
        old_ib: "3168da22",
        new_ib: "09bcb0fd",
        path: "./PlayerCharacterData/Lyney/hash.json"
    },
    {
        old_ib: "79a263de",
        new_ib: "41760901",
        path: "./PlayerCharacterData/Mika/hash.json"
    },
    {
        old_ib: "ef876207",
        new_ib: "d75308d8",
        path: "./PlayerCharacterData/Mona/hash.json"
    },
    {
        old_ib: "ed79ea5b",
        new_ib: "d5ad8084",
        path: "./PlayerCharacterData/MonaCN/hash.json"
    },
    {
        old_ib: "8b268d05",
        new_ib: "b3f2e7da",
        path: "./PlayerCharacterData/Nahida/hash.json"
    },
    {
        old_ib: "c8818002",
        new_ib: "f055eadd",
        path: "./PlayerCharacterData/Neuvillette/hash.json"
    },
    {
        old_ib: "265e34e3",
        new_ib: "1e8a5e3c",
        path: "./PlayerCharacterData/Nilou/hash.json"
    },
    {
        old_ib: "93085db7",
        new_ib: "abdc3768",
        path: "./PlayerCharacterData/Ningguang/hash.json"
    },
    {
        old_ib: "f1d09b47",
        new_ib: "c904f198",
        path: "./PlayerCharacterData/NingguangOrchid/hash.json"
    },
    {
        old_ib: "a4241241",
        new_ib: "9cf0789e",
        path: "./PlayerCharacterData/Noelle/hash.json"
    },
    {
        old_ib: "6ed110f3",
        new_ib: "56057a2c",
        path: "./PlayerCharacterData/Qiqi/hash.json"
    },
    {
        old_ib: "428c56cd",
        new_ib: "7a583c12",
        path: "./PlayerCharacterData/RaidenShogun/hash.json"
    },
    {
        old_ib: "23e2a216",
        new_ib: "1b36c8c9",
        path: "./PlayerCharacterData/Razor/hash.json"
    },
    {
        old_ib: "5d18b9d6",
        new_ib: "65ccd309",
        path: "./PlayerCharacterData/Rosaria/hash.json"
    },
    {
        old_ib: "851e4de1",
        new_ib: "bdca273e",
        path: "./PlayerCharacterData/RosariaCN/hash.json"
    },
    {
        old_ib: "eabbb767",
        new_ib: "d26fddb8",
        path: "./PlayerCharacterData/Sayu/hash.json"
    },
    {
        old_ib: "127ea5c9",
        new_ib: "2aaacf16",
        path: "./PlayerCharacterData/Sayu/hash.json"
    },
    {
        old_ib: "0b7d4e4d",
        new_ib: "33a92492",
        path: "./PlayerCharacterData/Shenhe/hash.json"
    },
    {
        old_ib: "c7c2a9d6",
        new_ib: "ff16c309",
        path: "./PlayerCharacterData/Shinobu/hash.json"
    },
    {
        old_ib: "3f71a2db",
        new_ib: "3f71a2db",
        path: "./PlayerCharacterData/Shinobu/hash.json"
    },
    {
        old_ib: "3ffb839e",
        new_ib: "",
        path: "./PlayerCharacterData/Sigewinne/hash.json"
    },
    {
        old_ib: "3e3c00b7",
        new_ib: "06e86a68",
        path: "./PlayerCharacterData/Sucrose/hash.json"
    },
    {
        old_ib: "8ac1328b",
        new_ib: "b2155854",
        path: "./PlayerCharacterData/Thoma/hash.json"
    },
    {
        old_ib: "bfeab9d1",
        new_ib: "",
        path: "./PlayerCharacterData/Tighnari/hash.json"
    },
    {
        old_ib: "b603af2f",
        new_ib: "8ed7c5f0",
        path: "./PlayerCharacterData/TravelerBoy/hash.json"
    },
    {
        old_ib: "dfb54407",
        new_ib: "e7612ed8",
        path: "./PlayerCharacterData/TravelerGirl/hash.json"
    },
    {
        old_ib: "c314953e",
        new_ib: "fbc0ffe1",
        path: "./PlayerCharacterData/Venti/hash.json"
    },
    {
        old_ib: "536e3b83",
        new_ib: "6bba515c",
        path: "./PlayerCharacterData/Wanderer/hash.json"
    },
    {
        old_ib: "99be9547",
        new_ib: "a16aff98",
        path: "./PlayerCharacterData/Wanderer/hash.json"
    },
    {
        old_ib: "a6b6de38",
        new_ib: "9e62b4e7",
        path: "./PlayerCharacterData/Wriothesley/hash.json"
    },
    {
        old_ib: "496a6d62",
        new_ib: "71be07bd",
        path: "./PlayerCharacterData/Wriothesley/hash.json"
    },
    {
        old_ib: "5363ff5d",
        new_ib: "6bb79582",
        path: "./PlayerCharacterData/Xiangling/hash.json"
    },
    {
        old_ib: "252781f1",
        new_ib: "1df3eb2e",
        path: "./PlayerCharacterData/Xiao/hash.json"
    },
    {
        old_ib: "ba1d11c3",
        new_ib: "82c97b1c",
        path: "./PlayerCharacterData/Xingqiu/hash.json"
    },
    {
        old_ib: "af23e644",
        new_ib: "97f78c9b",
        path: "./PlayerCharacterData/Xinyan/hash.json"
    },
    {
        old_ib: "65ddc0df",
        new_ib: "5d09aa00",
        path: "./PlayerCharacterData/Yae/hash.json"
    },
    {
        old_ib: "4fb839ef",
        new_ib: "776c5330",
        path: "./PlayerCharacterData/Yanfei/hash.json"
    },
    {
        old_ib: "54c0b1e8",
        new_ib: "6c14db37",
        path: "./PlayerCharacterData/YaoYao/hash.json"
    },
    {
        old_ib: "ba35247d",
        new_ib: "82e14ea2",
        path: "./PlayerCharacterData/Yelan/hash.json"
    },
    {
        old_ib: "bda31169",
        new_ib: "85777bb6",
        path: "./PlayerCharacterData/Yoimiya/hash.json"
    },
    {
        old_ib: "fee6d200",
        new_ib: "c632b8df",
        path: "./PlayerCharacterData/YunJin/hash.json"
    },
    {
        old_ib: "7450ea2a",
        new_ib: "4c8480f5",
        path: "./PlayerCharacterData/Zhongli/hash.json"
    },
    {
        old_ib: "7b8c754b",
        new_ib: "",
        path: "./SkillData/ArrowBow/hash.json"
    },
    {
        old_ib: "d20f2ad2",
        new_ib: "d20f2ad2",
        path: "./SkillData/BarbatosHolyLyre/hash.json"
    },
    {
        old_ib: "3180aa0c",
        new_ib: "0954c0d3",
        path: "./SkillData/CandaceShield/CandaceShield/hash.json"
    },
    {
        old_ib: "f1aeaaf7",
        new_ib: "c97ac028",
        path: "./SkillData/CatBox/hash.json"
    },
    {
        old_ib: "2c3e285e",
        new_ib: "",
        path: "./SkillData/CynoWolfMask/hash.json"
    },
    {
        old_ib: "10572338",
        new_ib: "",
        path: "./SkillData/CynoWolfMask/hash.json"
    },
    {
        old_ib: "a3a7d09b",
        new_ib: "14ea4281",
        path: "./SkillData/CynoWolfMask/hash.json"
    },
    {
        old_ib: "c6e4dd9b",
        new_ib: "fe30b744",
        path: "./SkillData/EulaECape/hash.json"
    },
    {
        old_ib: "b9b211e4",
        new_ib: "81667b3b",
        path: "./SkillData/FoxPole/hash.json"
    },
    {
        old_ib: "70e179c9",
        new_ib: "70e179c9",
        path: "./SkillData/IceSongofBrokenPines/hash.json"
    },
    {
        old_ib: "5e1761ff",
        new_ib: "5e1761ff",
        path: "./SkillData/IceSongofBrokenPines/hash.json"
    },
    {
        old_ib: "e3bcefbb",
        new_ib: "e3bcefbb",
        path: "./SkillData/IceSongofBrokenPines/hash.json"
    },
    {
        old_ib: "db1f13f2",
        new_ib: "db1f13f2",
        path: "./SkillData/KleeBombs/KleeBombs/BigBomb/hash.json"
    },
    {
        old_ib: "0fb5a354",
        new_ib: "3761c98b",
        path: "./SkillData/KleeBombs/KleeBombs/SmallBombBody/hash.json"
    },
    {
        old_ib: "4e3874c7",
        new_ib: "4e3874c7",
        path: "./SkillData/KleeBombs/KleeBombs/SmallBombFlower/hash.json"
    },
    {
        old_ib: "a6aa0aa0",
        new_ib: "a6aa0aa0",
        path: "./SkillData/KleeBombs/KleeBombs/SmallBombStem/hash.json"
    },
    {
        old_ib: "ea718d45",
        new_ib: "d2a5e79a",
        path: "./SkillData/LyneyHat/hash.json"
    },
    {
        old_ib: "34e18b4f",
        new_ib: "34e18b4f",
        path: "./SkillData/MoraxLapidis/hash.json"
    },
    {
        old_ib: "61ab7805",
        new_ib: "597f12da",
        path: "./SkillData/Oz/hash.json"
    },
    {
        old_ib: "4a9cbbf1",
        new_ib: "7248d12e",
        path: "./SkillData/YaoyaoYuegui/YaoyaoYuegui/hash.json"
    },
    {
        old_ib: "d37b4030",
        new_ib: "ebaf2aef",
        path: "./WeaponData/Bows/3Star/EbonyBow/hash.json"
    },
    {
        old_ib: "a1af1a03",
        new_ib: "997b70dc",
        path: "./WeaponData/Bows/3Star/Messenger/hash.json"
    },
    {
        old_ib: "3b3b2a3f",
        new_ib: "03ef40e0",
        path: "./WeaponData/Bows/3Star/RavenBow/hash.json"
    },
    {
        old_ib: "3b3b2a3f",
        new_ib: "03ef40e0",
        path: "./WeaponData/Bows/3Star/RecurveBow/hash.json"
    },
    {
        old_ib: "3b3b2a3f",
        new_ib: "03ef40e0",
        path: "./WeaponData/Bows/3Star/SharpshooterOath/hash.json"
    },
    {
        old_ib: "d37b4030",
        new_ib: "ebaf2aef",
        path: "./WeaponData/Bows/3Star/Slingshot/hash.json"
    },
    {
        old_ib: "3324d3a2",
        new_ib: "0bf0b97d",
        path: "./WeaponData/Bows/4Star/AlleyHunter/hash.json"
    },
    {
        old_ib: "ab7db00e",
        new_ib: "",
        path: "./WeaponData/Bows/4Star/BlackcliffWarbow/hash.json"
    },
    {
        old_ib: "cbf1af69",
        new_ib: "f325c5b6",
        path: "./WeaponData/Bows/4Star/CompoundBow/CompoundBow/hash.json"
    },
    {
        old_ib: "acc496e9",
        new_ib: "",
        path: "./WeaponData/Bows/4Star/EndOfTheLine/hash.json"
    },
    {
        old_ib: "2ac01106",
        new_ib: "12147bd9",
        path: "./WeaponData/Bows/4Star/FadingTwilight/hash.json"
    },
    {
        old_ib: "3324d3a2",
        new_ib: "0bf0b97d",
        path: "./WeaponData/Bows/4Star/FavoniusWarbow/hash.json"
    },
    {
        old_ib: "3c981d24",
        new_ib: "",
        path: "./WeaponData/Bows/4Star/Hamayumi/hash.json"
    },
    {
        old_ib: "3b800f1e",
        new_ib: "",
        path: "./WeaponData/Bows/4Star/KingSquire/hash.json"
    },
    {
        old_ib: "94bf9885",
        new_ib: "ac6bf25a",
        path: "./WeaponData/Bows/4Star/MitternachtWaltz/hash.json"
    },
    {
        old_ib: "a4120119",
        new_ib: "9cc66bc6",
        path: "./WeaponData/Bows/4Star/MouunsMoon/hash.json"
    },
    {
        old_ib: "3edc0658",
        new_ib: "06086c87",
        path: "./WeaponData/Bows/4Star/Predator/hash.json"
    },
    {
        old_ib: "8e183f2e",
        new_ib: "b6cc55f1",
        path: "./WeaponData/Bows/4Star/PrototypeAmber/hash.json"
    },
    {
        old_ib: "4b940e20",
        new_ib: "734064ff",
        path: "./WeaponData/Bows/4Star/RoyalBow/hash.json"
    },
    {
        old_ib: "3de5e3aa",
        new_ib: "05318975",
        path: "./WeaponData/Bows/4Star/Rust/hash.json"
    },
    {
        old_ib: "4b940e20",
        new_ib: "734064ff",
        path: "./WeaponData/Bows/4Star/SacrificialBow/hash.json"
    },
    {
        old_ib: "a358389b",
        new_ib: "9b8c5244",
        path: "./WeaponData/Bows/4Star/TheStringless/hash.json"
    },
    {
        old_ib: "d0cd3407",
        new_ib: "e8195ed8",
        path: "./WeaponData/Bows/4Star/TheViridescentHunt/hash.json"
    },
    {
        old_ib: "cbf1af69",
        new_ib: "f325c5b6",
        path: "./WeaponData/Bows/4Star/WindblumeOde/hash.json"
    },
    {
        old_ib: "8859cba5",
        new_ib: "b08da17a",
        path: "./WeaponData/Bows/5Star/AmosBow/hash.json"
    },
    {
        old_ib: "50133ce8",
        new_ib: "68c75637",
        path: "./WeaponData/Bows/5Star/AquaSimulacra/hash.json"
    },
    {
        old_ib: "e9206f1f",
        new_ib: "d1f405c0",
        path: "./WeaponData/Bows/5Star/ElegyForTheEnd/hash.json"
    },
    {
        old_ib: "0e9039c9",
        new_ib: "36445316",
        path: "./WeaponData/Bows/5Star/HuntersPath/hash.json"
    },
    {
        old_ib: "e8a6def8",
        new_ib: "d072b427",
        path: "./WeaponData/Bows/5Star/PolarStar/hash.json"
    },
    {
        old_ib: "c79eca00",
        new_ib: "ff4aa0df",
        path: "./WeaponData/Bows/5Star/SkywardHarp/hash.json"
    },
    {
        old_ib: "a7353c27",
        new_ib: "9fe156f8",
        path: "./WeaponData/Bows/5Star/ThunderingPulse/hash.json"
    },
    {
        old_ib: "117c694c",
        new_ib: "29a80393",
        path: "./WeaponData/Catalysts/3Star/AmberBead/hash.json"
    },
    {
        old_ib: "117c694c",
        new_ib: "29a80393",
        path: "./WeaponData/Catalysts/3Star/EmeraldOrb/hash.json"
    },
    {
        old_ib: "aa5642a4",
        new_ib: "9282287b",
        path: "./WeaponData/Catalysts/3Star/MagicGuide/hash.json"
    },
    {
        old_ib: "aa5642a4",
        new_ib: "9282287b",
        path: "./WeaponData/Catalysts/3Star/OtherworldlyStory/hash.json"
    },
    {
        old_ib: "aa5642a4",
        new_ib: "9282287b",
        path: "./WeaponData/Catalysts/3Star/ThrillingTalesOfDragonSlayer/hash.json"
    },
    {
        old_ib: "2d35e411",
        new_ib: "15e18ece",
        path: "./WeaponData/Catalysts/3Star/TwinNephrite/hash.json"
    },
    {
        old_ib: "8e183f2e",
        new_ib: "b6cc55f1",
        path: "./WeaponData/Catalysts/4Star/BlackcliffAgate/hash.json"
    },
    {
        old_ib: "0c71b7d3",
        new_ib: "34a5dd0c",
        path: "./WeaponData/Catalysts/4Star/DodocoTales/hash.json"
    },
    {
        old_ib: "0d8ce7af",
        new_ib: "35588d70",
        path: "./WeaponData/Catalysts/4Star/EyeOfPerception/hash.json"
    },
    {
        old_ib: "637fe5c9",
        new_ib: "5bab8f16",
        path: "./WeaponData/Catalysts/4Star/FavoniusCodex/hash.json"
    },
    {
        old_ib: "936908a5",
        new_ib: "",
        path: "./WeaponData/Catalysts/4Star/Frostbearer/hash.json"
    },
    {
        old_ib: "4be7772e",
        new_ib: "",
        path: "./WeaponData/Catalysts/4Star/FuitOfFultilment/hash.json"
    },
    {
        old_ib: "7c388d69",
        new_ib: "44ece7b6",
        path: "./WeaponData/Catalysts/4Star/HakushinRing/hash.json"
    },
    {
        old_ib: "0c71b7d3",
        new_ib: "34a5dd0c",
        path: "./WeaponData/Catalysts/4Star/MappaMare/hash.json"
    },
    {
        old_ib: "3aa3744d",
        new_ib: "02771e92",
        path: "./WeaponData/Catalysts/4Star/OathswornEye/hash.json"
    },
    {
        old_ib: "8e183f2e",
        new_ib: "b6cc55f1",
        path: "./WeaponData/Catalysts/4Star/PrototypeAmber/hash.json"
    },
    {
        old_ib: "958f3631",
        new_ib: "ad5b5cee",
        path: "./WeaponData/Catalysts/4Star/RoyalGrimoire/hash.json"
    },
    {
        old_ib: "958f3631",
        new_ib: "ad5b5cee",
        path: "./WeaponData/Catalysts/4Star/SacrificialFragments/hash.json"
    },
    {
        old_ib: "c1975d7e",
        new_ib: "b3ea9991",
        path: "./WeaponData/Catalysts/4Star/SolarPearl/hash.json"
    },
    {
        old_ib: "8b3ef34e",
        new_ib: "f94337a1",
        path: "./WeaponData/Catalysts/4Star/SolarPearl/hash.json"
    },
    {
        old_ib: "98c08a41",
        new_ib: "a014e09e",
        path: "./WeaponData/Catalysts/4Star/WanderingEvenstar/hash.json"
    },
    {
        old_ib: "57e004e6",
        new_ib: "6f346e39",
        path: "./WeaponData/Catalysts/4Star/Widsith/hash.json"
    },
    {
        old_ib: "637fe5c9",
        new_ib: "5bab8f16",
        path: "./WeaponData/Catalysts/4Star/WineAndSong/hash.json"
    },
    {
        old_ib: "3bfc1341",
        new_ib: "0328799e",
        path: "./WeaponData/Catalysts/5Star/aThousandFloatingDreams/hash.json"
    },
    {
        old_ib: "b2efcc7f",
        new_ib: "8a3ba6a0",
        path: "./WeaponData/Catalysts/5Star/EverlastingMoonglow/hash.json"
    },
    {
        old_ib: "2d27b256",
        new_ib: "15f3d889",
        path: "./WeaponData/Catalysts/5Star/JadefallsSplendor/hash.json"
    },
    {
        old_ib: "13342954",
        new_ib: "2be0438b",
        path: "./WeaponData/Catalysts/5Star/KagurasVerity/hash.json"
    },
    {
        old_ib: "0843477b",
        new_ib: "30972da4",
        path: "./WeaponData/Catalysts/5Star/LostPrayer/hash.json"
    },
    {
        old_ib: "d8d0e1e9",
        new_ib: "e0048b36",
        path: "./WeaponData/Catalysts/5Star/MemoryOfDust/hash.json"
    },
    {
        old_ib: "7d2473ab",
        new_ib: "45f01974",
        path: "./WeaponData/Catalysts/5Star/SkywardAtlas/hash.json"
    },
    {
        old_ib: "34f0bb53",
        new_ib: "0c24d18c",
        path: "./WeaponData/Catalysts/5Star/Tullaytullah'sRemembrance/hash.json"
    },
    {
        old_ib: "93a10c75",
        new_ib: "93a10c75",
        path: "./WeaponData/Claymores/1Star/WasterGreatsword/hash.json"
    },
    {
        old_ib: "93a10c75",
        new_ib: "93a10c75",
        path: "./WeaponData/Claymores/2Star/OldMercsPal/hash.json"
    },
    {
        old_ib: "efc62720",
        new_ib: "efc62720",
        path: "./WeaponData/Claymores/3Star/BloodtaintedGreatsword/hash.json"
    },
    {
        old_ib: "e79f95ef",
        new_ib: "e79f95ef",
        path: "./WeaponData/Claymores/3Star/DebateClub/hash.json"
    },
    {
        old_ib: "efc62720",
        new_ib: "efc62720",
        path: "./WeaponData/Claymores/3Star/FerrousShadow/hash.json"
    },
    {
        old_ib: "b2612e02",
        new_ib: "b2612e02",
        path: "./WeaponData/Claymores/3Star/Quartz/hash.json"
    },
    {
        old_ib: "b2612e02",
        new_ib: "b2612e02",
        path: "./WeaponData/Claymores/3Star/SkyriderGreatsword/hash.json"
    },
    {
        old_ib: "e4f9e501",
        new_ib: "e4f9e501",
        path: "./WeaponData/Claymores/3Star/WhiteIronGreatsword/hash.json"
    },
    {
        old_ib: "7a5888ca",
        new_ib: "7a5888ca",
        path: "./WeaponData/Claymores/4Star/Akuoumaru/hash.json"
    },
    {
        old_ib: "75eaaea1",
        new_ib: "75eaaea1",
        path: "./WeaponData/Claymores/4Star/Bell/hash.json"
    },
    {
        old_ib: "b45b28d4",
        new_ib: "8c8f420b",
        path: "./WeaponData/Claymores/4Star/BlackcliffSlasher/hash.json"
    },
    {
        old_ib: "21176dc0",
        new_ib: "21176dc0",
        path: "./WeaponData/Claymores/4Star/FavoniusGreatsword/hash.json"
    },
    {
        old_ib: "7edf08da",
        new_ib: "",
        path: "./WeaponData/Claymores/4Star/ForestRegalia/hash.json"
    },
    {
        old_ib: "dda33df1",
        new_ib: "",
        path: "./WeaponData/Claymores/4Star/Katsuragikiri/hash.json"
    },
    {
        old_ib: "1e471ae7",
        new_ib: "1e471ae7",
        path: "./WeaponData/Claymores/4Star/LithicBlade/hash.json"
    },
    {
        old_ib: "db2f1b0d",
        new_ib: "db2f1b0d",
        path: "./WeaponData/Claymores/4Star/MailedFlower/hash.json"
    },
    {
        old_ib: "b793be3a",
        new_ib: "b793be3a",
        path: "./WeaponData/Claymores/4Star/MakhairaAquamarine/hash.json"
    },
    {
        old_ib: "b45b28d4",
        new_ib: "8c8f420b",
        path: "./WeaponData/Claymores/4Star/PrototypeArchaic/hash.json"
    },
    {
        old_ib: "1e471ae7",
        new_ib: "1e471ae7",
        path: "./WeaponData/Claymores/4Star/Rainslasher/hash.json"
    },
    {
        old_ib: "3ab6c5d7",
        new_ib: "3ab6c5d7",
        path: "./WeaponData/Claymores/4Star/RoyalGreatsword/hash.json"
    },
    {
        old_ib: "3ab6c5d7",
        new_ib: "3ab6c5d7",
        path: "./WeaponData/Claymores/4Star/SacrificialGreatsword/hash.json"
    },
    {
        old_ib: "c8287f96",
        new_ib: "c8287f96",
        path: "./WeaponData/Claymores/4Star/SeaLord/hash.json"
    },
    {
        old_ib: "b140d661",
        new_ib: "b140d661",
        path: "./WeaponData/Claymores/4Star/SerpentSpine/hash.json"
    },
    {
        old_ib: "939e75f5",
        new_ib: "939e75f5",
        path: "./WeaponData/Claymores/4Star/SnowTombed/hash.json"
    },
    {
        old_ib: "4fc6fa1f",
        new_ib: "4fc6fa1f",
        path: "./WeaponData/Claymores/4Star/Whiteblind/hash.json"
    },
    {
        old_ib: "b6041a0b",
        new_ib: "b6041a0b",
        path: "./WeaponData/Claymores/5Star/BeaconOfTheReedSea/hash.json"
    },
    {
        old_ib: "e4011c08",
        new_ib: "e4011c08",
        path: "./WeaponData/Claymores/5Star/Redhorn/hash.json"
    },
    {
        old_ib: "75f971c6",
        new_ib: "75f971c6",
        path: "./WeaponData/Claymores/5Star/SkywardPride/hash.json"
    },
    {
        old_ib: "ae2bfaff",
        new_ib: "ae2bfaff",
        path: "./WeaponData/Claymores/5Star/SongOfBrokenPines/hash.json"
    },
    {
        old_ib: "af679171",
        new_ib: "af679171",
        path: "./WeaponData/Claymores/5Star/Unforged/hash.json"
    },
    {
        old_ib: "389602b9",
        new_ib: "389602b9",
        path: "./WeaponData/Claymores/5Star/WolfsGravestone/hash.json"
    },
    {
        old_ib: "7a232d03",
        new_ib: "7a232d03",
        path: "./WeaponData/Claymores/NonPlayer/IttoTaurusClaymore/hash.json"
    },
    {
        old_ib: "cfd98554",
        new_ib: "",
        path: "./WeaponData/Enemies/HilicurlRangerHydroScythe/hash.json"
    },
    {
        old_ib: "0dcfcb00",
        new_ib: "",
        path: "./WeaponData/Enemies/KairagiFierySword/hash.json"
    },
    {
        old_ib: "5b5bf57c",
        new_ib: "",
        path: "./WeaponData/Enemies/KairagiThunderSword/hash.json"
    },
    {
        old_ib: "98b74969",
        new_ib: "",
        path: "./WeaponData/Enemies/LineBreaker/hash.json"
    },
    {
        old_ib: "8673d264",
        new_ib: "",
        path: "./WeaponData/Enemies/MitaAxe/hash.json"
    },
    {
        old_ib: "506d8bde",
        new_ib: "d072b427",
        path: "./WeaponData/Enemies/PolarStars/hash.json"
    },
    {
        old_ib: "de8daa01",
        new_ib: "",
        path: "./WeaponData/Enemies/RagingTideHarbingers/hash.json"
    },
    {
        old_ib: "da4eed2c",
        new_ib: "",
        path: "./WeaponData/Enemies/StandardBearer/hash.json"
    },
    {
        old_ib: "29ea51e2",
        new_ib: "",
        path: "./WeaponData/Enemies/Windcutter/hash.json"
    },
    {
        old_ib: "cff1ba56",
        new_ib: "cff1ba56",
        path: "./WeaponData/Polearms/1Star/BeginnersProtector/hash.json"
    },
    {
        old_ib: "cff1ba56",
        new_ib: "cff1ba56",
        path: "./WeaponData/Polearms/2Star/IronPoint/hash.json"
    },
    {
        old_ib: "d3a49c0a",
        new_ib: "d3a49c0a",
        path: "./WeaponData/Polearms/3Star/BlackTassel/hash.json"
    },
    {
        old_ib: "c4e759de",
        new_ib: "c4e759de",
        path: "./WeaponData/Polearms/3Star/Halberd/hash.json"
    },
    {
        old_ib: "d3a49c0a",
        new_ib: "d3a49c0a",
        path: "./WeaponData/Polearms/3Star/WhiteTassel/hash.json"
    },
    {
        old_ib: "8e06dce4",
        new_ib: "b6d2b63b",
        path: "./WeaponData/Polearms/4Star/BlackcliffPole/hash.json"
    },
    {
        old_ib: "6a774547",
        new_ib: "6a774547",
        path: "./WeaponData/Polearms/4Star/CrescentPike/hash.json"
    },
    {
        old_ib: "23953878",
        new_ib: "23953878",
        path: "./WeaponData/Polearms/4Star/Deathmatch/hash.json"
    },
    {
        old_ib: "cc59be65",
        new_ib: "cc59be65",
        path: "./WeaponData/Polearms/4Star/DragonsBane/hash.json"
    },
    {
        old_ib: "e67288a0",
        new_ib: "e67288a0",
        path: "./WeaponData/Polearms/4Star/DragonspineSpear/hash.json"
    },
    {
        old_ib: "1cfef8f9",
        new_ib: "1cfef8f9",
        path: "./WeaponData/Polearms/4Star/FavoniusLance/hash.json"
    },
    {
        old_ib: "3ba4e2b4",
        new_ib: "3ba4e2b4",
        path: "./WeaponData/Polearms/4Star/Kitain/hash.json"
    },
    {
        old_ib: "cc59be65",
        new_ib: "cc59be65",
        path: "./WeaponData/Polearms/4Star/LithicSpear/hash.json"
    },
    {
        old_ib: "273e3a3b",
        new_ib: "273e3a3b",
        path: "./WeaponData/Polearms/4Star/MissiveWindspear/hash.json"
    },
    {
        old_ib: "535eb1ad",
        new_ib: "535eb1ad",
        path: "./WeaponData/Polearms/4Star/Moonpiercer/hash.json"
    },
    {
        old_ib: "8e06dce4",
        new_ib: "b6d2b63b",
        path: "./WeaponData/Polearms/4Star/PrototypeStarglitter/hash.json"
    },
    {
        old_ib: "7ced4ee6",
        new_ib: "",
        path: "./WeaponData/Polearms/4Star/RoyalSpear/hash.json"
    },
    {
        old_ib: "8d293d55",
        new_ib: "8d293d55",
        path: "./WeaponData/Polearms/4Star/TheCatch/hash.json"
    },
    {
        old_ib: "94c680c0",
        new_ib: "94c680c0",
        path: "./WeaponData/Polearms/4Star/WavebreakersFin/hash.json"
    },
    {
        old_ib: "5bf44f5e",
        new_ib: "5bf44f5e",
        path: "./WeaponData/Polearms/5Star/CalamityQueller/hash.json"
    },
    {
        old_ib: "e1ba0ff2",
        new_ib: "e1ba0ff2",
        path: "./WeaponData/Polearms/5Star/EngulfingLightning/hash.json"
    },
    {
        old_ib: "2979ad00",
        new_ib: "2979ad00",
        path: "./WeaponData/Polearms/5Star/JadeWingedSpear/hash.json"
    },
    {
        old_ib: "0a419395",
        new_ib: "0a419395",
        path: "./WeaponData/Polearms/5Star/SkywardSpine/hash.json"
    },
    {
        old_ib: "412330a9",
        new_ib: "412330a9",
        path: "./WeaponData/Polearms/5Star/StaffOfHoma/hash.json"
    },
    {
        old_ib: "b9138fed",
        new_ib: "b9138fed",
        path: "./WeaponData/Polearms/5Star/StaffOfTheScarletSands/hash.json"
    },
    {
        old_ib: "0804ee80",
        new_ib: "0804ee80",
        path: "./WeaponData/Polearms/5Star/VortexVanquisher/hash.json"
    },
    {
        old_ib: "e30ca5e9",
        new_ib: "",
        path: "./WeaponData/Polearms/NonPlayer/MoraxVortex/hash.json"
    },
    {
        old_ib: "a49b5447",
        new_ib: "a49b5447",
        path: "./WeaponData/Swords/1Star/DullBlade/hash.json"
    },
    {
        old_ib: "a49b5447",
        new_ib: "a49b5447",
        path: "./WeaponData/Swords/2Star/SilverSword/hash.json"
    },
    {
        old_ib: "dee2cf30",
        new_ib: "dee2cf30",
        path: "./WeaponData/Swords/3Star/CoolSteel/hash.json"
    },
    {
        old_ib: "a37b96c1",
        new_ib: "a37b96c1",
        path: "./WeaponData/Swords/3Star/DarkIronSword/hash.json"
    },
    {
        old_ib: "6557c7d9",
        new_ib: "6557c7d9",
        path: "./WeaponData/Swords/3Star/FilletBlade/hash.json"
    },
    {
        old_ib: "dee2cf30",
        new_ib: "dee2cf30",
        path: "./WeaponData/Swords/3Star/HarbingerOfDawn/hash.json"
    },
    {
        old_ib: "a37b96c1",
        new_ib: "a37b96c1",
        path: "./WeaponData/Swords/3Star/SkyriderSword/hash.json"
    },
    {
        old_ib: "d025c810",
        new_ib: "d025c810",
        path: "./WeaponData/Swords/3Star/TravelersHandySword/hash.json"
    },
    {
        old_ib: "0e69854d",
        new_ib: "0e69854d",
        path: "./WeaponData/Swords/4Star/AlleyFlash/hash.json"
    },
    {
        old_ib: "0e69854d",
        new_ib: "0e69854d",
        path: "./WeaponData/Swords/4Star/AlleyFlash/hash.json"
    },
    {
        old_ib: "34f01efb",
        new_ib: "0c247424",
        path: "./WeaponData/Swords/4Star/AmenomaKageuchi/hash.json"
    },
    {
        old_ib: "341c72d3",
        new_ib: "0cc8180c",
        path: "./WeaponData/Swords/4Star/BlackcliffLongsword/hash.json"
    },
    {
        old_ib: "bd404e6f",
        new_ib: "bd404e6f",
        path: "./WeaponData/Swords/4Star/BlackSword/hash.json"
    },
    {
        old_ib: "740b7b00",
        new_ib: "740b7b00",
        path: "./WeaponData/Swords/4Star/BrokenIsshinBlade/hash.json"
    },
    {
        old_ib: "1fed9b21",
        new_ib: "1fed9b21",
        path: "./WeaponData/Swords/4Star/CinnabarSpindle/hash.json"
    },
    {
        old_ib: "0e69854d",
        new_ib: "0e69854d",
        path: "./WeaponData/Swords/4Star/FavoniusSword/hash.json"
    },
    {
        old_ib: "caa58be1",
        new_ib: "caa58be1",
        path: "./WeaponData/Swords/4Star/FesteringDesire/hash.json"
    },
    {
        old_ib: "d7fb87ab",
        new_ib: "d7fb87ab",
        path: "./WeaponData/Swords/4Star/Flute/hash.json"
    },
    {
        old_ib: "c3442691",
        new_ib: "c3442691",
        path: "./WeaponData/Swords/4Star/IronSting/hash.json"
    },
    {
        old_ib: "740b7b00",
        new_ib: "740b7b00",
        path: "./WeaponData/Swords/4Star/KagotsurubeIsshin/hash.json"
    },
    {
        old_ib: "fad3a13e",
        new_ib: "fad3a13e",
        path: "./WeaponData/Swords/4Star/LionsRoar/hash.json"
    },
    {
        old_ib: "740b7b00",
        new_ib: "740b7b00",
        path: "./WeaponData/Swords/4Star/PrizedIsshinBlade/hash.json"
    },
    {
        old_ib: "341c72d3",
        new_ib: "0cc8180c",
        path: "./WeaponData/Swords/4Star/PrototypeRancour/hash.json"
    },
    {
        old_ib: "20f011e5",
        new_ib: "20f011e5",
        path: "./WeaponData/Swords/4Star/RoyalLongsword/hash.json"
    },
    {
        old_ib: "20f011e5",
        new_ib: "20f011e5",
        path: "./WeaponData/Swords/4Star/SacrificialSword/hash.json"
    },
    {
        old_ib: "4a0e68c8",
        new_ib: "4a0e68c8",
        path: "./WeaponData/Swords/4Star/SapwoodBlade/hash.json"
    },
    {
        old_ib: "4a74ca33",
        new_ib: "4a74ca33",
        path: "./WeaponData/Swords/4Star/SwordOfDescension/hash.json"
    },
    {
        old_ib: "31ac0c11",
        new_ib: "097866ce",
        path: "./WeaponData/Swords/4Star/Toukabou/hash.json"
    },
    {
        old_ib: "98fa1ef4",
        new_ib: "98fa1ef4",
        path: "./WeaponData/Swords/4Star/XiphosMoonlight/hash.json"
    },
    {
        old_ib: "4c6270ce",
        new_ib: "4c6270ce",
        path: "./WeaponData/Swords/5Star/AquilaFavonia/hash.json"
    },
    {
        old_ib: "33c15eef",
        new_ib: "33c15eef",
        path: "./WeaponData/Swords/5Star/FreedomSworn/hash.json"
    },
    {
        old_ib: "3e52464c",
        new_ib: "06862c93",
        path: "./WeaponData/Swords/5Star/HaranGeppaku/hash.json"
    },
    {
        old_ib: "4853de0b",
        new_ib: "4853de0b",
        path: "./WeaponData/Swords/5Star/JadeCutter/hash.json"
    },
    {
        old_ib: "e27ca3cd",
        new_ib: "e27ca3cd",
        path: "./WeaponData/Swords/5Star/KeyOfKhajNisut/hash.json"
    },
    {
        old_ib: "dd3e78ac",
        new_ib: "dd3e78ac",
        path: "./WeaponData/Swords/5Star/LightOfFoliarIncision/hash.json"
    },
    {
        old_ib: "914f8315",
        new_ib: "914f8315",
        path: "./WeaponData/Swords/5Star/Mistsplitter/hash.json"
    },
    {
        old_ib: "4035c5d6",
        new_ib: "4035c5d6",
        path: "./WeaponData/Swords/5Star/SkywardBlade/hash.json"
    },
    {
        old_ib: "8e208ff6",
        new_ib: "",
        path: "./WeaponData/Swords/5Star/SummitShaper/hash.json"
    },
    {
        old_ib: "c51d9d82",
        new_ib: "",
        path: "./WeaponData/Swords/NonPlayer/AetherSword/hash.json"
    },
    {
        old_ib: "998fb1e2",
        new_ib: "998fb1e2",
        path: "./WeaponData/Swords/NonPlayer/Alhaithamsword/hash.json"
    },
    {
        old_ib: "152ad73d",
        new_ib: "",
        path: "./WeaponData/Swords/NonPlayer/Dehya'sFather'sCane/hash.json"
    },
    {
        old_ib: "eb99ac83",
        new_ib: "",
        path: "./WeaponData/Swords/NonPlayer/Dehya'sFather'sCane/hash.json"
    },
    {
        old_ib: "81eb3ae6",
        new_ib: "",
        path: "./WeaponData/Swords/NonPlayer/LumineSword/hash.json"
    },
    {
        old_ib: "de4de508",
        new_ib: "de4de508",
        path: "./WeaponData/Swords/NonPlayer/RaidenEiSword/hash.json"
    },
    {
        old_ib: "fa2480c9",
        new_ib: "10c16643",
        path: ""
    },
    {
        old_ib: "72fce93f",
        new_ib: "4a2883e0",
        path: ""
    },
    {
        old_ib: "d0cee655",
        new_ib: "fc2de441",
        path: ""
    },
    {
        old_ib: "cd5cc84b",
        new_ib: "f588a294",
        path: ""
    },
    {
        old_ib: "f200e51c",
        new_ib: "cad48fc3",
        path: ""
    },
    {
        old_ib: "c210d17d",
        new_ib: "fac4bba2",
        path: ""
    },
    {
        old_ib: "b53993b4",
        new_ib: "8dedf96b",
        path: ""
    },
    {
        old_ib: "ab7fa7f0",
        new_ib: "93abcd2f",
        path: ""
    },
    {
        old_ib: "708a8fef",
        new_ib: "485ee530",
        path: ""
    },
    {
        old_ib: "c94185c7",
        new_ib: "f195ef18",
        path: ""
    },
    {
        old_ib: "30618ce1",
        new_ib: "08b5e63e",
        path: ""
    },
    {
        old_ib: "a2ad4f43",
        new_ib: "9a79259c",
        path: ""
    },
    {
        old_ib: "e2be38b9",
        new_ib: "da6a5266",
        path: ""
    },
    {
        old_ib: "38d56cf4",
        new_ib: "31b4048e",
        path: ""
    },
    {
        old_ib: "48f4e469",
        new_ib: "70208eb6",
        path: ""
    },
    {
        old_ib: "83b887d1",
        new_ib: "bb6ced0e",
        path: ""
    },
    {
        old_ib: "5594b4bb",
        new_ib: "6d40de64",
        path: ""
    },
    {
        old_ib: "6126ca2d",
        new_ib: "59f2a0f2",
        path: ""
    },
    {
        old_ib: "517c6d23",
        new_ib: "69a807fc",
        path: ""
    },
    {
        old_ib: "676cc015",
        new_ib: "5fb8aaca",
        path: ""
    },
    {
        old_ib: "6267d326",
        new_ib: "5ab3b9f9",
        path: ""
    },
    {
        old_ib: "4d4d5c3a",
        new_ib: "369498be",
        path: ""
    },
    {
        old_ib: "0e40f261",
        new_ib: "759936e5",
        path: ""
    },
    {
        old_ib: "61c2f10d",
        new_ib: "59169bd2",
        path: ""
    },
    {
        old_ib: "b6433558",
        new_ib: "8e975f87",
        path: ""
    },
    {
        old_ib: "104b1e83",
        new_ib: "289f745c",
        path: ""
    },
    {
        old_ib: "c966bc38",
        new_ib: "82e14ea2",
        path: ""
    },
    {
        old_ib: "bba8cec2",
        new_ib: "5d0c8b17",
        path: ""
    },
    {
        old_ib: "78734e37",
        new_ib: "64c3e455",
        path: ""
    },
    {
        old_ib: "87eb875c",
        new_ib: "37177107",
        path: ""
    },
    {
        old_ib: "5c6790ec",
        new_ib: "64b3fa33",
        path: ""
    }
];

// MARK: 4.4 FIX
const old_vs_new: Record<string, string> = {
    // Benny pos
    "993d1661": "6cff51b4",
    // Cyno posx
    "4cc92f60": "226f076e",
    // Fischl pos
    "9838aedf": "bf6aef4d",
    // Kirara pos
    "cc833025": "b57d7fe2",
    // Mona CN pos
    "ee5ed1dc": "515f3ce6",
    // Nahida all but draw
    "37ef15ec": "7106f05d",
    "bb58c686": "de60b92f",
    "f7b7ce48": "902b57ef",
    "b3f2e7da": "9b13c166",
    // Razor pos x
    "a099b935": "4662c505",
    // Xinyan pos
    "b3d31859": "76ed85f0",
    // CN Amber
    "7f94e8da": "557b2eff",
    // Xianyun
    '469f6119': '39838e8f',  // pose   // blend
    '55233416': '3e5e310f',   // textcoord
    'b8cfed01': '7f79ea6e',   // ib
    // Gaming 
    'cd9b5130': '1f9a91f4',   // blend
    'd4d3dc37': '47efcb75',  //blend
    '0d198dee': 'f6040d81', //blend
    //Layla
    'bafaa0e8': '72f035f8',   // blend
    //yelan3.4
    "f41a7730": "c58c76f9", //position
    "23c937b0": "f6e01e3c", //blend
    "0af017d0": "428b836c", //Texcoord
    //lisa3.6
    "7d8a4e0f": "2a557add",  //position
    "de1311ae": "8bfa989d",  //blend
    "50ae5602": "92b87c71",  //texcoord
    //Cyno 4.0
    "68416e15": "4cc92f60", //position
    "44d9d0f7": "6867e0b8", //blend 
    //Lisa Skin
    "3e6a5d39": "d77ffc4f", //Texcoord
    "531b64bc": "5db2f8f4", //blend
    "75e78f78": "37c70461", //Position
    // These need remap --------------------------------------------------
    // Venti allx
    "d56e66b4": "0d36f587",
    "635466ca": "09a91a5c",
    "fd47c861": "a6823bb3",
    "bf8270d3": "6568ac68",
    "fbc0ffe1": "1afcf31d",
    // Mona Global pos and blendx
    "20d0bfab": "7a1dc890",
    "52f0e9a0": "b043715a",
    // Ning Base
    "6d197625": "4c2f9a0a",
    "55b43e99": "f9e1b52b",
    "9f7dc19c": "735eaea4",
    "906ad233": "1f0ab400",
    "abdc3768": "ad75352c",
    // Diluc Base all
    "dff0b253": "5b0cb984",
    "6fdb0963": "71625c4d",
    "6fd20cc4": "afb527f6",
    "aee0755a": "6d0e22f0",
    "e9786c58": "e16fa548",
    // Xiao all
    "6f16afa7": "06c11756",
    "7f5ef8cc": "9464bf2d",
    "65a5e725": "258ab074",
    "ec61cc2b": "8ddd3ae9",
    "1df3eb2e": "ced409c1",
    // global Amber all but draw
    "caddc4c6": "a2ea4b2d",
    "ca5bd26e": "36d20a67",
    "e3047676": "81b777ca",
    "a1a2bbfb": "b03c7e30",
    //barbaba 3.6
    "02089582": "22a31278",
};

const remaps: Record<string, RemapData> = {
    "caddc4c6": {
        name: 'Amber Global',
        mapping: [7, 6, 9, 10, 11, 29, 8, 12, 13, 14, 15, 16, 17, 77, 1, 0,
            73, 18, 19, 20, 21, 53, 70, 74, 50, 30, 47, 51, 76, 75, 24,
            71, 28, 27, 54, 52, 31, 72, 55, 56, 61, 58, 62, 64, 65, 67,
            68, 57, 59, 60, 63, 66, 69, 48, 26, 25, 49, 32, 33, 38, 35,
            39, 41, 42, 44, 45, 34, 36, 37, 40, 43, 46, 22, 23, 2, 3,
            4, 5]
    },
    "52f0e9a0": {
        name: 'Mona Global',
        mapping: [37, 38, 39, 36, 58, 30, 31, 32, 29, 57, 26,
            25, 24, 27, 28, 34, 35, 40, 33, 81, 106, 102,
            47, 43, 46, 44, 42, 41, 45, 105, 104, 60, 54,
            53, 20, 83, 56, 55, 21, 15, 10, 16, 11, 5, 23,
            0, 6, 1, 22, 77, 49, 50, 100, 51, 52, 79, 2, 7,
            3, 4, 8, 9, 12, 17, 13, 14, 18, 19, 59, 76, 82,
            99, 80, 103, 48, 101, 84, 90, 87, 93, 96, 97, 85,
            86, 88, 89, 91, 92, 94, 95, 98, 78, 61, 67, 64, 70,
            73, 74, 62, 63, 65, 66, 68, 69, 71, 72, 75,]
    },
    "6d197625": {
        name: 'Ningguang Base',
        mapping: [70, 6, 20, 21, 22, 12, 13, 14, 15, 16, 7, 17,
            18, 19, 8, 9, 10, 11, 4, 5, 69, 54, 53, 94, 111, 115, 71, 88,
            92, 91, 52, 114, 116, 117, 118, 95, 93, 67, 72, 65, 68, 60, 61,
            62, 63, 64, 66, 55, 56, 57, 58, 59, 40, 44, 45, 32, 23, 29, 24,
            33, 37, 38, 30, 89, 90, 73, 79, 76, 82, 85, 86, 74, 75, 77, 78,
            80, 81, 83, 84, 87, 2, 0, 112, 113, 96, 102, 99, 105, 108, 109,
            97, 98, 100, 101, 103, 104, 106, 107, 110, 41, 43, 48, 28, 3,
            1, 31, 36, 35, 34, 26, 27, 25, 39, 49, 51, 47, 46, 42, 50,]
    },
    "dff0b253": {
        name: 'Diluc Base',
        mapping: [77, 78, 79, 57, 37, 4, 9, 6, 7, 8, 10, 5, 39, 32,
            31, 53, 54, 49, 50, 51, 46, 47, 48, 43, 44, 45, 40, 41, 42, 80,
            81, 82, 59, 34, 33, 73, 74, 69, 70, 71, 66, 67, 68, 63, 64, 65,
            60, 61, 62, 83, 84, 85, 1, 3, 2, 0, 21, 22, 25, 26, 27, 13, 14,
            15, 16, 28, 29, 30, 17, 18, 19, 20, 23, 24, 12, 55, 38, 52, 56,
            75, 58, 72, 76, 35, 36, 11]
    },
    "7f5ef8cc": {
        name: 'Xiao',
        mapping: [8, 47, 37, 7, 12, 13, 14, 10, 9, 11, 71, 48, 91, 38, 93,
            68, 88, 92, 65, 69, 95, 72, 45, 49, 43, 70, 94, 44, 67, 66, 50,
            56, 53, 59, 62, 63, 51, 52, 54, 55, 57, 58, 60, 61, 64, 39, 90,
            89, 74, 73, 79, 76, 82, 85, 86, 75, 77, 78, 80, 81, 83, 84, 87,
            40, 42, 41, 5, 36, 6, 33, 30, 29, 26, 25, 35, 34, 31, 32, 28, 27,
            15, 16, 17, 18, 19, 21, 22, 23, 24, 20, 0, 1, 4, 3, 2, 46,]
    },
    "fd47c861": {
        name: 'Venti',
        mapping: [114, 115, 116, 91, 68, 49, 50, 47, 48, 51, 52, 53, 54,
            55, 56, 93, 110, 111, 94, 95, 96, 97, 98, 99, 100, 101, 102, 103,
            104, 105, 106, 107, 108, 65, 66, 39, 40, 43, 44, 25, 26, 27, 28,
            29, 19, 24, 20, 21, 22, 23, 70, 87, 88, 71, 72, 73, 74, 75, 76, 77,
            78, 79, 80, 81, 82, 83, 84, 85, 64, 63, 7, 8, 9, 10, 11, 41, 42, 37,
            38, 1, 2, 3, 4, 5, 6, 46, 45, 12, 13, 14, 15, 16, 17, 18, 30, 31, 32,
            33, 34, 35, 36, 0, 89, 69, 86, 90, 112, 92, 109, 113, 67, 57, 58, 59,
            60, 61, 62,]
    },
    "02089582": {
        name: 'Barbara',
        mapping: [54, 12, 8, 9, 77, 17, 18, 19, 13, 14, 10, 11, 15, 16, 53, 100, 101, 102, 56, 49, 7, 6, 79, 51, 20, 75, 98, 55, 78, 1, 2, 0, 3, 4, 5, 38, 37, 30,
            29, 45, 33, 39, 31, 32, 41, 21, 22, 25, 26, 34, 42, 76, 72, 99, 95, 73, 74, 57, 50, 63, 64, 60, 66, 67, 69, 70, 58, 59, 61, 62, 65, 68, 71, 96, 97, 80, 52, 86, 87, 83,
            89, 90, 92, 93, 81, 82, 84, 85, 88, 91, 94, 44, 43, 36, 35, 24, 23, 28, 27, 40, 48, 47, 46,]
    }
};

const oldvsnew_41: Record<string, string> = {};
const oldvsnew_43: Record<string, string> = {};
const DetermineCharacterBasedOnPositionHash_41: Record<string, string> = {};

for (const j of alljson_41) {
    if (j.new_draw_vb !== "") {
        if (j.old_draw_vb !== "") {
            oldvsnew_41[j.old_draw_vb] = j.new_draw_vb;
        }
        DetermineCharacterBasedOnPositionHash_41[j.position_vb] = j.new_draw_vb;
    }
}

for (const j of alljson_43) {
    if (j.new_ib !== "") {
        if (j.old_ib !== "") {
            oldvsnew_43[j.old_ib] = j.new_ib;
        }
    }
}

function createBackup(filePath: string, isIni: boolean = false): string | null {
    try {
        let backupPath: string;
        if (isIni) {
            backupPath = filePath.replace('.ini', '.txt');
        } else {
            const dir = path.dirname(filePath);
            const fileName = path.basename(filePath);
            const backupName = `Original_${fileName}.bak`;
            backupPath = path.join(dir, backupName);
        }

        fs.copyFileSync(filePath, backupPath);
        return backupPath;
    } catch (error) {
        console.error(`Error creating backup for ${filePath}: ${error}`);
        return null;
    }
}

function collectIniFiles(folderPath: string, excludeDisabled: boolean = false): string[] {
    console.log("\nCollecting ini files, please wait...");
    const iniFiles: string[] = [];
    const excludeKeywords = new Set(['desktop', 'ntuser', 'disabled_backup']);

    if (excludeDisabled) {
        excludeKeywords.add('disabled');
    }

    function walkDir(dir: string) {
        const files = fs.readdirSync(dir);

        for (const file of files) {
            const fullPath = path.join(dir, file);
            const stat = fs.statSync(fullPath);

            if (stat.isDirectory()) {
                if (!fullPath.includes('BufferValues')) {
                    walkDir(fullPath);
                }
            } else if (file.toLowerCase().endsWith('.ini')) {
                const shouldExclude = Array.from(excludeKeywords).some(keyword =>
                    file.toLowerCase().includes(keyword)
                );
                if (!shouldExclude) {
                    iniFiles.push(fullPath);
                }
            }
        }
    }

    walkDir(folderPath);
    return iniFiles;
}

function remap(blendData: Buffer, vgRemap: number[]): Buffer {
    const stride = 32;
    if (blendData.length % stride !== 0) {
        throw new Error("Invalid blend file");
    }

    const remappedBlend = Buffer.from(blendData);

    for (let i = 0; i < blendData.length; i += stride) {
        for (let x = 0; x < 4; x++) {
            const weight = blendData.readFloatLE(i + x * 4);
            const index = blendData.readUInt32LE(i + 16 + x * 4);

            if (weight !== 0 && index < vgRemap.length) {
                const newIndex = vgRemap[index];
                remappedBlend.writeUInt32LE(newIndex, i + 16 + x * 4);
            }
        }
    }

    return remappedBlend;
}

function applyRemap(
    data: string,
    folderPath: string,
    blendFiles: string[],
    remaps: Record<string, RemapData>,
    reverse: boolean,
    noRemap: boolean
): string {
    for (const [x, vgRemap] of Object.entries(remaps)) {
        if (data.includes(x)) {
            if (noRemap) {
                console.log(`Skipping remap for ${vgRemap.name} in folder: ${path.basename(folderPath)} due to no_remap argument`);
                continue;
            }

            const sameFolderBlends = blendFiles.filter(blendFile =>
                path.dirname(blendFile) === folderPath && !blendFile.endsWith('.bak')
            );

            if (sameFolderBlends.length === 0) {
                console.log(`No blend file found for ${vgRemap.name} in folder: ${path.basename(folderPath)}`);
                continue;
            }

            console.log(`Found blend files for ${vgRemap.name}. Applying remap...`);

            for (const blendFile of sameFolderBlends) {
                try {
                    if (reverse) {
                        const bakFile = `Original_${path.basename(blendFile)}.bak`;
                        const bakPath = path.join(folderPath, bakFile);

                        if (fs.existsSync(bakPath)) {
                            fs.unlinkSync(blendFile);
                            fs.renameSync(bakPath, blendFile);
                            console.log(`Restored original file: ${path.basename(blendFile)}`);
                        } else {
                            console.log(`No backup found for ${path.basename(blendFile)}. Skipping.`);
                            continue;
                        }
                    } else {
                        const backupPath = createBackup(blendFile);
                        if (backupPath) {
                            console.log(`Backup created: ${path.basename(backupPath)}`);
                        }

                        const blendData = fs.readFileSync(blendFile);
                        const remapData = remap(blendData, vgRemap.mapping);
                        fs.writeFileSync(blendFile, remapData);
                        console.log(`File: ${path.basename(blendFile)} VGs remapped successfully!`);
                    }
                } catch (error) {
                    console.error(`Error processing file: ${path.basename(blendFile)}`);
                    console.error(error);
                }
            }
        }
    }

    return data;
}

function updateVersion(
    data: string,
    oldvsnew: Record<string, string> | Record<string, Record<string, Record<string, string>>>,
    version: string,
    jsonData?: JsonData41[]
): string {
    let updated = false;
    let alreadyUpdated = false;

    if (typeof oldvsnew === 'object' && Object.values(oldvsnew).every(v => typeof v === 'object' && !Array.isArray(v))) {
        // Handle 5.0 structure
        const nested = oldvsnew as Record<string, Record<string, Record<string, string>>>;
        for (const [category, characters] of Object.entries(nested)) {
            for (const [character, hashes] of Object.entries(characters)) {
                for (const [old, newHash] of Object.entries(hashes)) {
                    if (old === newHash && data.includes(old)) {
                        alreadyUpdated = true;
                        break;
                    } else if (data.includes(old)) {
                        data = data.replace(new RegExp(old, 'g'), newHash);
                        console.log(`\t[FIX ${version}] (${category} - ${character}): ${old} -> ${newHash}`);
                        updated = true;
                    }
                }
            }
        }
    } else {
        const simple = oldvsnew as Record<string, string>;
        for (const [old, newHash] of Object.entries(simple)) {
            if (old === newHash) {
                if (data.includes(old)) {
                    alreadyUpdated = true;
                    break;
                }
            } else if (data.includes(old)) {
                data = data.replace(new RegExp(old, 'g'), newHash);
                let objectName = "Unknown";

                if (version === "4.1" && jsonData) {
                    try {
                        const matchingItems = jsonData.filter(item =>
                            item.old_draw_vb === old || item.position_vb === old
                        );
                        if (matchingItems.length > 0 && matchingItems[0].path) {
                            objectName = matchingItems[0].path.split('/').slice(-2)[0];
                        }
                    } catch (error) {
                        // ignore error
                    }
                }

                const objectInfo = version === "4.1" ? ` (Object: ${objectName})` : "";
                console.log(`\t[FIX ${version}]: ${old} -> ${newHash}${objectInfo}`);
                updated = true;
            }
        }
    }

    if (alreadyUpdated) {
        console.log(`\tAlready Updated ${version}`);
    } else if (!updated) {
        console.log(`\tAlready Updated ${version}`);
    }

    return data;
}

function processIniFile(
    iniFile: string,
    blendFiles: string[],
    remaps: Record<string, RemapData>,
    config: ProcessingConfig
): ProcessingResult {
    let updated = false;

    try {
        const folderPath = path.dirname(iniFile);
        const relativePath = path.relative(process.cwd(), path.dirname(iniFile));
        console.log(`Processing INI file: ${path.basename(iniFile)} (Path: ${relativePath})`);

        let data = fs.readFileSync(iniFile, 'utf-8');
        const originalData = data;

        if (config.reverse) {
            if (config.fix_54) {
                const reverseMap: Record<string, string> = {};
                for (const category of Object.values(config.old_vs_new_54)) {
                    for (const character of Object.values(category)) {
                        for (const [k, v] of Object.entries(character)) {
                            reverseMap[v] = k;
                        }
                    }
                }
                data = updateVersion(data, reverseMap, "5.4 (Reverse)");
            }
            if (config.fix_44_47) {
                const reverseMap: Record<string, string> = {};
                for (const [k, v] of Object.entries(config.old_vs_new)) {
                    reverseMap[v] = k;
                }
                data = updateVersion(data, reverseMap, "4.4 - 4.7 (Reverse)");
            }
            if (config.fix_43) {
                const reverseMap: Record<string, string> = {};
                for (const [k, v] of Object.entries(config.oldvsnew_43)) {
                    reverseMap[v] = k;
                }
                data = updateVersion(data, reverseMap, "4.3 (Reverse)");
            }
            if (config.fix_41) {
                data = applyRemap(data, folderPath, blendFiles, remaps, config.reverse, config.no_remap);
                const reverseMap: Record<string, string> = {};
                for (const [k, v] of Object.entries(config.oldvsnew_41)) {
                    reverseMap[v] = k;
                }
                data = updateVersion(data, reverseMap, "4.1 (Reverse)", alljson_41);
            }
        } else {
            if (config.fix_41) {
                data = updateVersion(data, config.oldvsnew_41, "4.1", alljson_41);
                data = applyRemap(data, folderPath, blendFiles, remaps, config.reverse, config.no_remap);
            }
            if (config.fix_43) {
                data = updateVersion(data, config.oldvsnew_43, "4.3");
            }
            if (config.fix_44_47) {
                data = updateVersion(data, config.old_vs_new, "4.4 - 4.7");
            }
            if (config.fix_54) {
                data = updateVersion(data, config.old_vs_new_54, "5.4");
            }
        }

        if (data !== originalData) {
            const backupPath = createBackup(iniFile, true);
            if (backupPath) {
                console.log(`Backup created: ${path.basename(backupPath)}`);
            }

            fs.writeFileSync(iniFile, data, 'utf-8');
            console.log(`File: ${path.basename(iniFile)} has been modified!`);
            updated = true;
        } else {
            console.log(`File: ${path.basename(iniFile)} had no matches. Skipping`);
        }

    } catch (error) {
        console.error(`Error processing file: ${path.basename(iniFile)}`);
        console.error(error);
    }

    return { updated, log: [] };
}

export async function fixGenshinMod(folderPath: string, options: {
    fix_41?: boolean;
    fix_43?: boolean;
    fix_44_47?: boolean;
    fix_54?: boolean;
    reverse?: boolean;
    no_remap?: boolean;
    exclude_disabled?: boolean;
} = {}) {
    const startTime = Date.now();

    if (!folderPath.includes("Mods")) {
        console.error("Error: The directory path must contain a 'Mods' folder.");
        return;
    }

    // Default to all fixes if none specified
    const versionFixes = {
        fix_41: options.fix_41 ?? true,
        fix_43: options.fix_43 ?? true,
        fix_44_47: options.fix_44_47 ?? true,
        fix_54: options.fix_54 ?? true
    };

    const config: ProcessingConfig = {
        ...versionFixes,
        oldvsnew_41,
        oldvsnew_43,
        old_vs_new,
        old_vs_new_54,
        reverse: options.reverse ?? false,
        no_remap: options.no_remap ?? false
    };

    if (config.reverse) {
        console.log("=".repeat(70));
        console.log('WARNING: You are running the script in reverse mode.');
        console.log('This will attempt to revert changes made by previous runs of this script.');
        console.log('This operation can potentially cause issues if files have been modified since the last run.');
        console.log("=".repeat(70));
    }

    const iniFiles = collectIniFiles(folderPath, options.exclude_disabled);
    const blendFiles: string[] = [];

    function collectBlendFiles(dir: string) {
        const files = fs.readdirSync(dir);
        for (const file of files) {
            const fullPath = path.join(dir, file);
            const stat = fs.statSync(fullPath);

            if (stat.isDirectory()) {
                collectBlendFiles(fullPath);
            } else if (file.toLowerCase().includes("blend") && file.toLowerCase().includes(".buf")) {
                blendFiles.push(fullPath);
            }
        }
    }

    collectBlendFiles(folderPath);

    let processedFilesCount = 0;

    for (const iniFile of iniFiles) {
        const result = processIniFile(iniFile, blendFiles, remaps, config);
        if (result.updated) {
            processedFilesCount++;
        }
        console.log("=".repeat(70));
    }

    const elapsedTime = (Date.now() - startTime) / 1000;
    console.log(`\nProcessing took ${elapsedTime.toFixed(2)} seconds`);
    console.log(`Total INI files found: ${iniFiles.length}`);
    console.log(`Processed ${processedFilesCount} files`);
}