import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';

export const version = '3.2T';

// Error and warning strings
const ERROR_STR = '\x1b[1m\x1b[0;31mError:\x1b[0m';
const WARN_STR = '\x1b[1m\x1b[1;33mWarning:\x1b[0m';

// Valid hash trios - "NamePart": [blend_hash, draw_hash, pos_hash]
const VALID_HASH_TRIOS: Record<string, [string, string, string]> = {
    "AnaxaHair": ["1db60089", "bdae3c65", "468acfca"],
    "AnaxaHead": ["69bc8d92", "be62cf9d", "aa48fbc5"],
    "AnaxaBody": ["4d4f5629", "4de13263", "c61a8bbe"],
    "AnaxaEyepatch": ["e5134739", "63df34d9", "ab5a904e"],
    "HoolayBody": ["ecaf8218", "d8ab2873", "1e3c634b"],
    "NatashaBottle": ["01abb5f5", "51296d22", "ee20ea77"],
    "HookWeapon": ["867cf70b", "d5b36d31", "55814c1b"],
    "HoolaySkirt": ["007fe09b", "3ec1c747", "d72347a1"],
    "Sam": ["d254e3e6", "6f49347e", "dcbe881e"],
    "SilvermaneSoldierBody": ["253b0ea5", "fa72be5d", "9e4fb633"],
    "Svarog": ["859f113a", "ac8cc541", "ea026e06"],
    "HoolayAlterHair": ["405006eb", "5fd8b48f", "b555ab06"],
    "HoolayAlterHead": ["b2d612b8", "b102c66e", "19e7e650"],
    "HoolayAlterBody": ["993e667e", "1d5cd97b", "a622a191"],
    "HuaiyanHair": ["3b3ba200", "1b91cb26", "25f76a06"],
    "HuaiyanHead": ["2a92a60d", "cfe5e63e", "edc4d39e"],
    "HuaiyanBody": ["2d447b02", "a1293b29", "fa222ecb"],
    "MicahHair": ["ff5828e2", "06c76108", "6191ae1e"],
    "MicahHead": ["d385ede2", "1318a7fa", "b085e628"],
    "MicahBody": ["3ee5103c", "a533a853", "e69242d5"],
    "Numby": ["8c0cfabb", "d9b4999a", "9afaa7d9"],
    "Pom-PomHead": ["9e29db14", "89346e66", "4df12bea"],
    "Pom-PomEyes": ["18070922", "246e67b3", "e21c3bb9"],
    "Pom-PomBody": ["553fbd51", "3b30e01d", "c558cdd5"],
    "RecaHair": ["3781d5f1", "930e080c", "e07d524f"],
    "RecaHead": ["5cb96afe", "3b530692", "683c5640"],
    "RecaBody": ["67cb45cf", "0481d916", "b7766576"],
    "SiobhanHair": ["1d60c420", "a5effef5", "122f9c8f"],
    "SiobhanHead": ["337f6223", "bf15a183", "f1a2cd99"],
    "SiobhanBody": ["41b400fe", "55dd912b", "e7b4d744"],
    "SundayHair": ["18bd0d10", "aed42577", "d3bcb943"],
    "SundayHead": ["93be6016", "891a3809", "1673f94c"],
    "SundayBody": ["2a306b59", "10a3d37b", "379567c3"],
    "TauranHair": ["a19ea453", "50d14cf6", "d0c8cc99"],
    "TauranHead": ["352d4b3b", "980c30cd", "2bfb8cad"],
    "TauranBody": ["fcebd5fa", "1fb38728", "32bf6e33"],
    "AcheronHair": ["01912bb7", "1e8e72ab", "8ef0c1e2"],
    "AcheronHairBack": ["0b9522ed", "64b0f338", "4fc4180d"],
    "AcheronHairBackUltimate": ["fe3d9074", "7ef00088", "36c43580"],
    "AcheronHead": ["17489664", "0f1b35ea", "90dbef5c"],
    "AcheronBody": ["9de39691", "f9910926", "a52830c5"],
    "AglaeaHair": ["67673667", "1e0c0119", "ee2ee8ee"],
    "AglaeaHead": ["6ed5a76f", "4ee730fd", "c7ad0566"],
    "AglaeaBody": ["e0fe90e1", "6b23e8ee", "6534f21b"],
    "GarmentmakerBody": ["de296bba", "32d27cc1", "51fe4da6"],
    "GarmentmakerSword": ["fcb94763", "c497ad84", "c6f151fe"],
    "ArgentiHair": ["23eb66a3", "a31b0feb", "6172337a"],
    "ArgentiHead": ["0f3bb2ae", "03a7a014", "0a503d26"],
    "ArgentiBody": ["d0637428", "9254e4a7", "8938505f"],
    "ArlanHair": ["565d39f1", "a25d5385", "735b44ab"],
    "ArlanHead": ["8bdc409f", "05d01e8f", "3cb31245"],
    "ArlanBody": ["458ce9a9", "835494dc", "b7db63bc"],
    "AstaHair": ["4925ec87", "1335fa30", "8fc91aeb"],
    "AstaHead": ["60227cf7", "6e1e2998", "769b1d76"],
    "AstaBody": ["19ac7cd3", "0483ee84", "86253b68"],
    "AventurineHair": ["341e4698", "25892278", "9453410b"],
    "AventurineHead": ["3981443a", "72661265", "b437c182"],
    "AventurineBody": ["61e0c51b", "a62e1fae", "6bffa54d"],
    "BailuHair": ["873f8612", "a7cc99f5", "0eccc78d"],
    "BailuHead": ["fa514aa8", "c4617c31", "135483ef"],
    "BailuBody": ["7955d970", "e19e70f3", "fb4e6152"],
    "BlackSwanHair": ["e2770c9a", "d236e57b", "c5a02152"],
    "BlackSwanHead": ["0219d5e4", "7acd2899", "2a58d400"],
    "BlackSwanBody": ["5b1d7cc9", "a51a0a3e", "094e2119"],
    "BladeHair": ["4547483a", "863caa28", "c48584d3"],
    "BladeHead": ["c4313cec", "47c1b5e5", "89966060"],
    "BladeBody": ["a3371520", "5aa9d7f5", "cd89693e"],
    "BoothillHair": ["4224575a", "c5357838", "c8d1ea4f"],
    "BoothillHead": ["0b64b229", "1774a115", "9910c4dc"],
    "BoothillBody": ["be7282b3", "fdf20423", "8dbe6204"],
    "BronyaHair": ["22b00312", "41a14ff6", "5bd0bd35"],
    "BronyaHead": ["debecdf7", "2fd9f85e", "8ead35ad"],
    "BronyaBody": ["e638702b", "a5ea3d65", "1147cb6f"],
    "CaelusDestructionHair": ["8877a03c", "3ba4eb10", "9eb07943"],
    "CaelusDestructionHead": ["ce50d7b6", "13e27600", "9e47ee7c"],
    "CaelusDestructionBody": ["c45ad87e", "3ca71eeb", "aa2648d4"],
    "CaelusHarmonyHair": ["8877a03c", "3ba4eb10", "9eb07943"],
    "CaelusHarmonyHead": ["da87925e", "13e27600", "9e47ee7c"],
    "CaelusHarmonyBody": ["c45ad87e", "3ca71eeb", "aa2648d4"],
    "CaelusPreservationHair": ["8877a03c", "3ba4eb10", "9eb07943"],
    "CaelusPreservationHead": ["8d6ae530", "13e27600", "9e47ee7c"],
    "CaelusPreservationBody": ["c45ad87e", "3ca71eeb", "aa2648d4"],
    "CaelusRemembranceHair": ["8877a03c", "3ba4eb10", "9eb07943"],
    // "CaelusRemembranceHead": ["8d6ae530", "13e27600", "9e47ee7c"], // repeat
    "CaelusRemembranceBody": ["c45ad87e", "3ca71eeb", "aa2648d4"],
    "CastoriceHair": ["776a0dad", "e6d1f87b", "e28bb3dd"],
    "CastoriceHead": ["e7ea12e0", "e31bfa54", "e852e6ad"],
    "CastoriceBody": ["e79ca63a", "cc41f6a0", "f1254d05"],
    "Pollux": ["4dc15128", "13e3587d", "46cbcb27"],
    "PolluxHands": ["dd930dac", "8acc42a5", "8a6cee3b"],
    "ClaraHair": ["38e27206", "b3683156", "51da74b2"],
    "ClaraHead": ["ee32d324", "ce483868", "cbe7c508"],
    "ClaraBody": ["6615fbf6", "830f5c83", "f4ad6f23"],
    "DanHengHair": ["ececdf7d", "a36a40a7", "ac24c7e5"],
    "DanHengHead": ["b40d1033", "c00f4d4c", "97637138"],
    "DanHengBody": ["58a834e9", "23c966a6", "48aa5ec1"],
    "DanHengILHair": ["784f7415", "20fa8734", "1061f077"],
    "DanHengILHead": ["a9e7afb3", "eac59924", "dec00008"],
    "DanHengILBody": ["734cb0a9", "d3b29940", "093e22f4"],
    "DanHengILHorns": ["844d098a", "1db75b43", "d9262263"],
    "DrRatioHair": ["92c6de75", "bc83a613", "6328101e"],
    "DrRatioHead": ["81ecb824", "97c4cd9f", "a138d574"],
    "DrRatioBody": ["03a0eb05", "da642b03", "053732e7"],
    "FeixiaoHair": ["5a68340d", "75041858", "2af4924a"],
    "FeixiaoHead": ["d19bdf98", "0cc8a20d", "e26c62d6"],
    "FeixiaoBody": ["9fb44ee9", "a29c89d1", "9c653391"],
    "FeixiaoMark": ["0b654360", "b81da035", "20d10710"],
    "FireflyHair": ["9670ee1a", "186136d6", "c95ed721"],
    "FireflyHead": ["5a3bee9a", "33f2dacf", "794f8dd5"],
    "FireflyHeadExtra": ["94a5b64e", "33f2dacf", "794f8dd5"],
    "FireflyBody": ["87e84993", "d511a04e", "f8fbf6ce"],
    "FugueHair": ["bcc387b9", "bcf9c26d", "9f2ec900"],
    "FugueHead": ["82946198", "10c9078f", "80d6a09e"],
    "FugueBody": ["7b083f20", "cf67bb57", "64a2b2e4"],
    "FugueTail": ["eb2187f4", "a90245f5", "f917214d"],
    "FuXuanHair": ["60ed7b28", "8b2930fc", "4696bceb"],
    "FuXuanHead": ["5eefba15", "e4fb727f", "e16dc49c"],
    "FuXuanBody": ["d0aaa833", "5d090c39", "62bdf52f"],
    "GallagherHair": ["36259335", "4373d23e", "a9ac91a3"],
    "GallagherHead": ["e5855ae5", "05a980f0", "b5d1317e"],
    "GallagherBody": ["57c512df", "6dacb01d", "46f13636"],
    "GepardHair": ["10cb3951", "030fa511", "3fc651d0"],
    "GepardHead": ["d802ea61", "4ee730fd", "007cd17b"],
    "GepardBody": ["7784a1c2", "6b730b83", "20cf413e"],
    "GuinaifenHair": ["9184be3a", "fb960197", "02f9db09"],
    "GuinaifenHead": ["d8a2c5ba", "b7c0dfd9", "6ae8ee30"],
    "GuinaifenBody": ["7c08b8a8", "6ce9e7d6", "49dbc0a2"],
    "HanyaHair": ["4ead3a1b", "c9d12e3c", "09203665"],
    "HanyaHead": ["bce0b15e", "a10a513e", "b44daf6c"],
    "HanyaBody": ["cf715cb3", "c16cb300", "be3b14b5"],
    "HertaHair": ["af0ef73c", "f67e9060", "34c19736"],
    "HertaHead": ["2e65ff34", "0e8c035a", "8a6c9639"],
    "HertaBody": ["d7ccfed7", "cf1012f5", "6b999ed4"],
    "HimekoHair": ["9e50b6bf", "738da645", "b1451388"],
    "HimekoHead": ["67f84104", "30f6c118", "8513ffc7"],
    "HimekoBody": ["7b8abb2a", "6d460e12", "4ba7ff4b"],
    "HookHair": ["be912ce7", "5c438f67", "35b79857"],
    "HookHead": ["e5b070b8", "ef3973a2", "64c5d9d3"],
    "HookBody": ["20825705", "bf053fd2", "84dd6df9"],
    "HuoHuoHair": ["858d0339", "dbb6a31f", "38013bf1"],
    "HuoHuoHead": ["264d043a", "99456749", "653ef435"],
    "HuoHuoBody": ["4e40d4ae", "68334db0", "5b0744cf"],
    "JadeHair": ["7c4b5250", "0ac36fa2", "ff9e3e28"],
    "JadeHead": ["9f87dc13", "b416a575", "55bfc174"],
    "JadeBody": ["08f5f722", "a6379d5f", "b0fed430"],
    "JiaoqiuHair": ["f301ae38", "36ad1e7e", "7fcebcf7"],
    "JiaoqiuHead": ["7e34a071", "b416a575", "55c052de"],
    "JiaoqiuBody": ["4554deab", "771c74f6", "5d046eeb"],
    "JingliuHair": ["41fc8ade", "1e6c1d97", "2c47650c"],
    "JingliuHead": ["0301cb1b", "9aad247f", "76235489"],
    "JingliuBody": ["24c37e04", "7c4d555b", "09a74ad8"],
    "JingYuanHair": ["0e2638a8", "4aecf045", "288482ed"],
    "JingYuanHead": ["8e240fa1", "c717230f", "84d5b158"],
    "JingYuanBody": ["23a9ca05", "04c1a42a", "5209eaca"],
    "KafkaHair": ["7ee24742", "1cb6a0c8", "d46f6a11"],
    "KafkaHead": ["07e0c801", "47c458ae", "650b9edf"],
    "KafkaBody": ["a45ac58d", "62d6efc5", "d4973f4a"],
    "LingshaHair": ["538904b8", "b47b8583", "dbb2bd24"],
    "LingshaHead": ["336dc44c", "34c0339f", "feccef92"],
    "LingshaBody": ["05bd3b66", "9b982e14", "b1dd664a"],
    "LukaHair": ["067d4370", "9cdf77af", "b0ded734"],
    "LukaHead": ["8ebc8e77", "a75cb220", "3b9e8be2"],
    "LukaBody": ["d012ae01", "bf9f2dd9", "27e0c525"],
    "LuochaHead": ["ed675449", "73ebad81", "cc41f294"],
    "LuochaBody": ["e5ea5c1e", "16d95198", "d16e462c"],
    "LuochaHair": ["bb75aef0", "a2f89b5c", "36ce9f22"],
    "LynxHair": ["f6ac4107", "ebfcbddb", "630c8211"],
    "LynxHead": ["31a96dae", "4fee1a45", "af83c9c4"],
    "LynxBody": ["156d0dec", "b502f29b", "6296fe54"],
    "March7thHuntHair": ["80b7f06b", "96a52a9a", "58a13862"],
    "March7thHuntHead": ["86be292f", "4635947e", "ab8d7452"],
    "March7thHuntBody": ["9edad201", "58f7fff6", "e0454a9f"],
    "March7thPreservationHair": ["9b6d1345", "b1bcbbd4", "1025233c"],
    "March7thPreservationHead": ["86be292f", "4635947e", "ab8d7452"],
    "March7thPreservationBody": ["555fd4a4", "e9f678b6", "f5491fea"],
    "March7thPreservationSpringHair": ["9b6d1345", "b1bcbbd4", "1025233c"],
    "March7thPreservationSpringHead": ["86be292f", "4635947e", "ab8d7452"],
    "March7thPreservationSpringBody": ["a8df1f65", "395e1926", "1ec246a7"],
    "MemBody": ["1066f7e6", "d07f89f0", "65e20e67"],
    "MishaHair": ["d513823d", "c25783a1", "b6957108"],
    "MishaHead": ["766c8127", "595d0add", "a73bfbf5"],
    "MishaBody": ["934ddec8", "69c18ac5", "d5d9dd08"],
    "MozeHair": ["a6a233d8", "8ff677d1", "d13afd55"],
    "MozeHead": ["e809f29d", "c9d12e3c", "5168ced4"],
    "MozeBody": ["6992723f", "045fb822", "590ae82e"],
    "MydeiHair": ["afcb6c89", "24998a31", "b3b22e61"],
    "MydeiHead": ["9db22d69", "5172a57d", "8c41a4d5"],
    "MydeiBody": ["2506e1cf", "d2fa0357", "4aaeda33"],
    "NatashaHair": ["d717ec93", "4125daaa", "30e68621"],
    "NatashaHead": ["7bc35491", "a2f89b5c", "53ac6996"],
    "NatashaBody": ["cc378bf8", "b185764c", "50edbe41"],
    "PelaHair": ["a4e6feef", "174a988f", "e4912e8e"],
    "PelaHead": ["bcf67460", "886a8cce", "c2b02016"],
    "PelaBody": ["5778182b", "7637086e", "27b7176f"],
    "QingqueHair": ["f10d8832", "7829ac5c", "8bd9c5cc"],
    "QingqueHead": ["639eb729", "c60480e3", "8f3ad331"],
    "QingqueBody": ["ff66678e", "3e8e9f4a", "8020c78e"],
    "RappaHair": ["f92b563c", "2f79ddcc", "cc675469"],
    "RappaHead": ["51946886", "4f97843a", "00e7776f"],
    "RappaBody": ["7d6fab35", "fb3c1ddb", "92db0310"],
    "RobinHair": ["eddc18ae", "07f14e1d", "dfecc1c0"],
    "RobinHead": ["8719e42d", "31a91f87", "5704688d"],
    "RobinBody": ["7b519a06", "5343b4c1", "3b5cf498"],
    "RuanMeiHair": ["d4fdc6c2", "96a52a9a", "676d566c"],
    "RuanMeiHead": ["a6fb174f", "9d8b73a6", "432d0696"],
    "RuanMeiBody": ["c2ccef0e", "172e7ba6", "67fa6522"],
    "SamBody": ["cf00dd15", "33ab0bd9", "7e2c7aac"],
    "SamShoulderEffect": ["bf1f0928", "d9a21081", "f21ba070"],
    "SampoHair": ["3b5a1c83", "3ed74e5c", "292065de"],
    "SampoHead": ["1c01b7d4", "69c57562", "22fec1ad"],
    "SampoBody": ["56c66568", "82a86474", "2087b78d"],
    "SeeleHair": ["46f042fa", "c97a209c", "175906e5"],
    "SeeleHead": ["060ed3c1", "446bbc7d", "f8177eed"],
    "SeeleBody": ["65645e5d", "4a62ad63", "bb8ec098"],
    "ServalHair": ["b43b4790", "4fafaaea", "40a80b53"],
    "ServalHead": ["689a54d8", "dd137cff", "345c20f9"],
    "ServalBody": ["4481e0a3", "84539e80", "026affd1"],
    "SilverWolfHair": ["b98c76ea", "26a98961", "4bb60956"],
    "SilverWolfHead": ["c305160c", "b3683156", "28f40811"],
    "SilverWolfBody": ["f8611a03", "64213ba5", "bd2443af"],
    "SparkleHair": ["a1b60c00", "206f41aa", "422fee08"],
    "SparkleHead": ["a782cbc4", "bd2f4472", "db79f5dc"],
    "SparkleBody": ["7e488505", "cdbefc09", "9588097c"],
    "SparkleIdleMask": ["", "d1bb0a3c", "d1bb0a3c"],
    "StelleHair": ["a91c061e", "0f43f610", "95b91a3d"],
    "StelleHead": ["aa50f051", "9cfbc761", "19d09218"],
    "StelleBody": ["b55c8431", "d52d7139", "f05d06de"],
    "SushangHair": ["d7d84752", "897cc269", "d1c9ece1"],
    "SushangHead": ["3fa7fdd7", "e35c5db1", "b4dc5fa9"],
    "SushangBody": ["1cfe532b", "708db391", "df4128f2"],
    "TheHertaHair": ["7419ffaa", "2deaf0ab", "606ff67f"],
    "TheHertaHead": ["22382f0e", "158aac84", "6d322515"],
    "TheHertaBody": ["bb291719", "f68aa938", "9d7d1fea"],
    "TingyunHair": ["ab3f980c", "02d2f998", "a7b248fd"],
    "TingyunHead": ["109ea4b0", "72dcbea3", "e04f6c8d"],
    "TingyunBody": ["e96f9618", "65d006d1", "b061758a"],
    "TopazHair": ["24266d75", "c3143284", "bda6a391"],
    "TopazHead": ["97f059cc", "7bc40221", "f90790b7"],
    "TopazBody": ["ba1eeb80", "92cfe440", "68668027"],
    "TribbieHair": ["9222c252", "d1164e62", "e7f18814"],
    "TribbieHead": ["2507ca60", "396ae8c6", "6461d2e9"],
    "TribbieBody": ["f992d345", "9f177bf0", "c9acfea5"],
    "TrianneHead": ["c5e924b2", "c3a0a99f", "26b0b5cc"],
    "TrinnonHead": ["da34931b", "6dde528f", "fd080aa6"],
    "WeltHair": ["db18ed41", "2a61be98", "d8893d9c"],
    "WeltHead": ["e55ea64d", "a9598daa", "95181dfd"],
    "WeltBody": ["45b357f7", "c1945568", "45f9ba4b"],
    "XueyiHair": ["0f09b035", "45e60399", "43620d38"],
    "XueyiHead": ["a55a4d71", "21d92a9f", "b55935f1"],
    "XueyiBody": ["7ed6d9e7", "9c0f0f9d", "908358a9"],
    "YanqingHair": ["8d7b4700", "1aa7e433", "bb5b36f7"],
    "YanqingHead": ["db1ae1d8", "0beb189f", "42744ec9"],
    "YanqingBody": ["af73f238", "e0453b30", "b9254412"],
    "YukongHair": ["dde8b84e", "013abaae", "8ff29179"],
    "YukongHead": ["0ac88a9a", "d731e230", "05fef99a"],
    "YukongBody": ["e2d592d3", "e121317e", "59dca58e"],
    "YunliHair": ["cd55b811", "06b925ae", "75d5fafb"],
    "YunliHead": ["51f8529b", "6dcdbe7f", "5d12f931"],
    "YunliBody": ["c540afe9", "13d57de1", "94e38803"],
};

// Interfaces
interface INILine {
    key: string;
    value: string;
    isValuePair: boolean;
    strippedLowerKey: string;
    strippedLowerValue: string;
}

interface Section {
    name: string;
    lines: INILine[];
    isHeader: boolean;
}

interface Resource {
    name: string;
    type: string;
    filename: string;
    stride: number;
}

interface ModelData {
    partName: string;
    blendResource: Resource;
    posResource: Resource;
    refDrawHash: string;
    refBlendHash: string;
    vertcount: number;
    blendConsumed: boolean;
    drawConsumed: boolean;
    resConsumed: boolean;
}

interface CommandListCandidate {
    hasVb0: boolean;
    commandList: Section | null;
    drawHash: string;
    blendHash: string;
    drawSectionPatched: boolean;
}

// Utility functions
function createINILine(key: string, value: string = "", isValuePair: boolean = false): INILine {
    return {
        key,
        value,
        isValuePair,
        strippedLowerKey: key.trim().toLowerCase(),
        strippedLowerValue: value.trim().toLowerCase()
    };
}

function hasKey(line: INILine, key: string): boolean {
    return line.strippedLowerKey === key.trim().toLowerCase();
}

function keyStartsWith(line: INILine, key: string): boolean {
    return line.strippedLowerKey.startsWith(key.trim().toLowerCase());
}

function createSection(name: string, lines: INILine[] = [], isHeader: boolean = false): Section {
    return { name, lines, isHeader };
}

function hasName(section: Section, name: string): boolean {
    return section.name.trim().toLowerCase().slice(1).replace(/]$/, '') === name.trim().toLowerCase();
}

function nameStartsWith(section: Section, name: string): boolean {
    if (section.name.length === 0) return false;
    return section.name.trim().toLowerCase().slice(1).startsWith(name.trim().toLowerCase());
}

function addLines(section: Section, lines: string): void {
    clearEmptyEndingLines(section);
    const lineArray = lines.split(/\r?\n/);
    for (const line of lineArray) {
        addSingleLine(section, line + '\n');
    }
    clearEmptyEndingLines(section);
    addSingleLine(section, '\n\n');
}

function addSingleLine(section: Section, line: string): void {
    const keyValue = line.split('=');
    if (keyValue.length === 2) {
        const key = keyValue[0];
        const value = keyValue[1];
        section.lines.push(createINILine(key, value, true));
    } else {
        section.lines.push(createINILine(line, '', false));
    }
}

function clearEmptyEndingLines(section: Section): void {
    while (section.lines.length > 0 && section.lines[section.lines.length - 1].key.trim() === '') {
        section.lines.pop();
    }
}

function commentOut(section: Section): void {
    clearEmptyEndingLines(section);
    section.name = `;${section.name}`;
    for (const line of section.lines) {
        line.key = `;${line.key}`;
    }
    addSingleLine(section, '\n\n');
}

function createResource(name: string, type: string, filename: string, stride: number = 0): Resource {
    return { name, type, filename, stride };
}

function createModelData(
    partName: string,
    blendResource: Resource,
    posResource: Resource,
    refDrawHash: string,
    refBlendHash: string,
    vertcount: number
): ModelData {
    return {
        partName,
        blendResource,
        posResource,
        refDrawHash,
        refBlendHash,
        vertcount,
        blendConsumed: false,
        drawConsumed: false,
        resConsumed: false
    };
}

function createCommandListCandidate(): CommandListCandidate {
    return {
        hasVb0: false,
        commandList: null,
        drawHash: '',
        blendHash: '',
        drawSectionPatched: false
    };
}

function splitInSections(content: string): Section[] {
    const sections: Section[] = [];
    const lines = content.split(/\r?\n/);
    let currentSection = createSection('', [], true);

    for (const line of lines) {
        const strippedLine = line.trim();
        if (strippedLine.startsWith('[') && strippedLine.endsWith(']')) {
            if (currentSection) {
                sections.push(currentSection);
            }
            const sectionName = line;
            currentSection = createSection(sectionName, []);
            continue;
        }
        addSingleLine(currentSection, line + '\n');
    }

    if (currentSection) {
        sections.push(currentSection);
    }

    return sections;
}

function reconstructINIFile(sections: Section[]): string {
    const content: string[] = [];

    for (const section of sections) {
        if (!section.isHeader) {
            content.push(section.name);
        }
        for (const line of section.lines) {
            if (line.isValuePair) {
                content.push(`${line.key}=${line.value}`);
            } else {
                content.push(line.key);
            }
        }
    }

    return content.join('');
}

function backupAndWrite(oldBody: string, newBody: string, filePath: string, toPrint: string[]): void {
    const backupFilePath = filePath.replace(/\.ini$/, '.txt');

    try {
        fs.writeFileSync(backupFilePath, oldBody, 'utf8');
        fs.writeFileSync(filePath, newBody, 'utf8');
    } catch (error) {
        toPrint.push(`Error writing to file: ${error}`);
        return;
    }

    toPrint.push(`Backup created at ${backupFilePath}`);
}

function restoreBackup(filePath: string, toPrint: string[]): void {
    const backupFilePath = filePath.replace(/\.ini$/, '.txt');

    if (!fs.existsSync(backupFilePath)) {
        toPrint.push(`\tNo backup found for ${filePath}.`);
        return;
    }

    try {
        const content = fs.readFileSync(backupFilePath, 'utf8');
        fs.writeFileSync(filePath, content, 'utf8');
        fs.unlinkSync(backupFilePath);
    } catch (error) {
        toPrint.push(`\tError restoring backup: ${error}`);
    } finally {
        toPrint.push(`\tRestored ${filePath} from ${backupFilePath}.`);
    }
}

function getResourceData(section: Section): [string, number, string] {
    const name = section.name.trim().slice(1).replace(/]$/, '');
    let stride = 0;
    let filename = '';

    for (const line of section.lines) {
        if (hasKey(line, 'stride')) {
            stride = parseInt(line.value.trim());
        } else if (hasKey(line, 'filename')) {
            filename = line.value.trim();
        }
    }

    return [name, stride, filename];
}

function splitInIfElseBlocks(lines: INILine[]): INILine[][] {
    const ifElseBlocks: INILine[][] = [];
    let currentBlock: INILine[] = [];
    let depth = -1;

    for (const line of lines) {
        if (keyStartsWith(line, 'if')) {
            depth++;
        } else if (keyStartsWith(line, 'elif') || keyStartsWith(line, 'else') || keyStartsWith(line, 'elseif')) {
            if (depth === 0 && currentBlock.length > 0) {
                ifElseBlocks.push(currentBlock);
                currentBlock = [];
            }
        } else if (keyStartsWith(line, 'endif')) {
            depth--;
            if (depth === -1 && currentBlock.length > 0) {
                ifElseBlocks.push(currentBlock);
                currentBlock = [];
            }
        }
        currentBlock.push(line);
    }

    if (currentBlock.length > 0) {
        ifElseBlocks.push(currentBlock);
    }

    return ifElseBlocks;
}

function attemptCommandListPosBlendPatch(
    sections: Section[],
    commandList: string,
    toPrint: string[],
    parentSection: Section,
    blendHash: string
): boolean {
    for (const section of sections) {
        if (!hasName(section, commandList)) {
            continue;
        }

        const ifElseBlocks = splitInIfElseBlocks(section.lines);
        if (ifElseBlocks.length <= 1) {
            toPrint.push(`${WARN_STR} ${commandList} doesn't have if else blocks. Probably not a merge mod...`);
            return false;
        }

        toPrint.push(`Found ${commandList} with ifelse blocks. Attempting to patch...`);

        for (const block of ifElseBlocks) {
            const ifElseTemplate = `{condition}
handling = skip
vb2 = {blend_resource}
if DRAW_TYPE == 1
    vb0 = {pos_resource}
    draw = {vertcount}, 0
endif
{rest_of_section}`;

            let condition: INILine | null = null;
            let blendResource = '';
            let posResource = '';
            let vertcount = 0;
            const restOfBlock: INILine[] = [];

            if (!(keyStartsWith(block[0], 'if') ||
                keyStartsWith(block[0], 'else') ||
                keyStartsWith(block[0], 'elif')) &&
                ifElseBlocks.length > 1) {
                continue;
            }

            condition = block[0];

            for (let i = 1; i < block.length; i++) {
                const line = block[i];
                if (hasKey(line, 'handling')) {
                    continue;
                }
                if (hasKey(line, 'vb0')) {
                    posResource = line.value.trim();
                } else if (hasKey(line, 'vb2')) {
                    blendResource = line.value.trim();
                } else if (hasKey(line, 'draw')) {
                    if (!line.value.includes(',')) {
                        continue;
                    }
                    vertcount = parseInt(line.value.split(',')[0].trim());
                } else {
                    restOfBlock.push(line);
                }
            }

            if (posResource === '' || blendResource === '' || vertcount === 0) {
                toPrint.push(`${ERROR_STR} Missing resource values in ${commandList} block. Can't patch merge mod.`);
                continue;
            }

            const tempSection = createSection('', [], true);
            const blockStr = ifElseTemplate
                .replace('{condition}', condition.key.trim())
                .replace('{blend_resource}', blendResource)
                .replace('{pos_resource}', posResource)
                .replace('{vertcount}', vertcount.toString())
                .replace('{rest_of_section}', reconstructINIFile([createSection('', restOfBlock, true)]));

            addLines(tempSection, blockStr);
            clearEmptyEndingLines(tempSection);

            block.length = 0;
            block.push(...tempSection.lines);
        }

        // Reconstruct section with new blocks
        section.lines.length = 0;
        for (const block of ifElseBlocks) {
            section.lines.push(...block);
        }
        clearEmptyEndingLines(section);
        addSingleLine(section, '\n');

        for (const line of parentSection.lines) {
            if (hasKey(line, 'hash')) {
                line.value = ` ${blendHash}\n`;
            }
        }

        parentSection.name = parentSection.name.replace('Position', 'Blend');

        toPrint.push(`Patched ${commandList} with blend hash ${blendHash}.`);
        return true;
    }

    toPrint.push(`${WARN_STR} Failed to fetch commandlist ${commandList}.`);
    return false;
}

function posToBlendModdingFix(content: string, toPrint: string[]): Section[] {
    const sections = splitInSections(content);

    const blendTemplate = `
hash = {blend_hash}
handling = skip
vb2 = {blend_resource}
if DRAW_TYPE == 1
    vb0 = {pos_resource}
    draw = {vertcount}, 0
endif
{rest_of_section}`;

    let isMergedMod = false;

    for (const section of sections) {
        if (!nameStartsWith(section, 'textureoverride')) {
            continue;
        }

        let sectionHash = '';
        let blendHash = '';

        try {
            sectionHash = section.lines
                .filter(line => hasKey(line, 'hash'))
                .map(line => line.strippedLowerValue)[0];

            const matchingTrio = Object.values(VALID_HASH_TRIOS)
                .find(([_b, _d, p]) => sectionHash === p);

            if (!matchingTrio) {
                continue;
            }

            blendHash = matchingTrio[0];
        } catch (error) {
            continue;
        }

        // Check if blend override already exists
        const blendOverrideExists = sections.some(s =>
            s.lines.some(line =>
                hasKey(line, 'hash') && line.strippedLowerValue === blendHash
            )
        );

        if (blendOverrideExists) {
            commentOut(section);
            toPrint.push(`${WARN_STR} ${section.name.trim()} already has a blend override, commenting out current section because is probably useless. Skipping part...`);
            continue;
        }

        let posResource = '';
        let blendResource = '';
        let vertcount = 0;
        const restOfLines: string[] = [];
        let commandListFound: string | null = null;

        for (const line of section.lines) {
            const strippedValue = line.value.trim().toLowerCase();

            if (hasKey(line, 'handling') || hasKey(line, 'hash')) {
                continue;
            }

            if (hasKey(line, 'vb0')) {
                posResource = line.value.trim();
            } else if (hasKey(line, 'vb2')) {
                blendResource = line.value.trim();
            } else if (hasKey(line, 'draw')) {
                if (!strippedValue.includes(',')) {
                    continue;
                }
                vertcount = parseInt(strippedValue.split(',')[0].trim());
            } else if (hasKey(line, 'run') && strippedValue.startsWith('commandlist')) {
                commandListFound = line.value.trim();
                toPrint.push(`Found CommandList ${commandListFound} in ${section.name.trim()}. Checking if we are in a merge mod.`);
            } else {
                if (line.isValuePair) {
                    restOfLines.push(`${line.key}=${line.value}`);
                } else {
                    restOfLines.push(line.key);
                }
            }
        }

        // Handle Caelus head special case
        if (sectionHash === '9e47ee7c') {
            toPrint.push(`${WARN_STR} ${section.name.trim()} is a Caelus Head. Fix might fail, please verify manually.`);

            const sectionNameLower = section.name.toLowerCase();
            if (sectionNameLower.includes('destruction')) {
                blendHash = 'ce50d7b6';
            } else if (sectionNameLower.includes('harmony')) {
                blendHash = 'da87925e';
            } else if (sectionNameLower.includes('preservation')) {
                blendHash = '8d6ae530';
            } else if (sectionNameLower.includes('remembrance')) {
                blendHash = '8d6ae530';
            } else {
                toPrint.push(`${ERROR_STR} ${section.name.trim()} is a Caelus Head but couldn't detect correct path. Skipping part...`);
                continue;
            }
        }

        if (commandListFound) {
            if (attemptCommandListPosBlendPatch(sections, commandListFound, toPrint, section, blendHash)) {
                isMergedMod = true;
                continue;
            } else {
                toPrint.push(`${ERROR_STR} ${section.name.trim()} doesn't have a valid format. Skipping...`);
                continue;
            }
        }

        if (isMergedMod) {
            toPrint.push(`${WARN_STR} ${section.name.trim()} is in a merge mod but doesn't have commandlist. Skipping part...`);
            continue;
        }

        if (posResource === '' || blendResource === '' || vertcount === 0) {
            toPrint.push(`${ERROR_STR} Missing resource values in ${section.name.trim()}. Skipping part...`);
            continue;
        }

        if (restOfLines.length > 0 && restOfLines[restOfLines.length - 1] !== '\n') {
            restOfLines.push('\n');
        }

        const blendStr = blendTemplate
            .replace('{blend_hash}', blendHash)
            .replace('{pos_resource}', posResource)
            .replace('{blend_resource}', blendResource)
            .replace('{vertcount}', vertcount.toString())
            .replace('{rest_of_section}', restOfLines.join(''))
            .substring(1); // Remove leading newline

        section.lines.length = 0;
        addLines(section, blendStr);

        const oldName = section.name.trim();
        section.name = section.name.replace('Position', 'Blend');

        toPrint.push(`Patched ${oldName}(${sectionHash}) -> ${section.name.trim()}(${blendHash}).`);
    }

    return sections;
}

function gatherModelData(
    sections: Section[],
    blendSections: Section[],
    toPrint: string[]
): ModelData[] {
    const modelList: ModelData[] = [];

    for (const blendSection of blendSections) {
        let vertcount = 1;
        let posRef = '';
        let blendRef = '';
        let blendHash = '';

        for (const line of blendSection.lines) {
            if (hasKey(line, 'draw')) {
                if (!line.value.includes(',')) {
                    toPrint.push(`${ERROR_STR} Invalid draw value in ${blendSection.name}. Skipping part...`);
                    continue;
                }
                vertcount = parseInt(line.value.split(',')[0].trim());
            } else if (hasKey(line, 'hash')) {
                blendHash = line.value.trim().toLowerCase();
            } else if (hasKey(line, 'vb0')) {
                posRef = line.value.trim();
            } else if (hasKey(line, 'vb2')) {
                blendRef = line.value.trim();
            }
        }

        if (blendHash === '' || posRef === '' || blendRef === '') {
            toPrint.push(`${ERROR_STR} Missing hash or resource values in ${blendSection.name}. Skipping part...`);
            continue;
        }

        const posResSection = sections.filter(s =>
            posRef.toLowerCase().trim() === s.name.toLowerCase().trim().slice(1).replace(/]$/, '')
        );

        const blendResSection = sections.filter(s =>
            blendRef.toLowerCase().trim() === s.name.toLowerCase().trim().slice(1).replace(/]$/, '')
        );

        if (posResSection.length === 0 || blendResSection.length === 0) {
            toPrint.push(`${ERROR_STR} Missing resource sections for ${blendSection.name}. Skipping part...`);
            continue;
        }

        if (posResSection.length + blendResSection.length !== 2) {
            toPrint.push(`${ERROR_STR} Multiple resource sections for ${blendSection.name}. Unable to decide which to use. Skipping part...`);
            continue;
        }

        const [posName, posStride, posFile] = getResourceData(posResSection[0]);
        const [blendName, blendStride, blendFile] = getResourceData(blendResSection[0]);

        let drawFound = '';
        try {
            drawFound = Object.values(VALID_HASH_TRIOS)
                .find(([b, _d, _p]) => b === blendHash)?.[1] || '';
        } catch (error) {
            continue;
        }

        const partName = Object.entries(VALID_HASH_TRIOS)
            .find(([_name, [b, _d, _p]]) => blendHash === b)?.[0] || '';

        modelList.push(createModelData(
            partName,
            createResource(blendName + 'CS', 'StructuredBuffer', blendFile, blendStride),
            createResource(posName + 'CS', 'StructuredBuffer', posFile, posStride),
            drawFound,
            blendHash,
            vertcount
        ));
    }

    return modelList;
}

function checkModelData(
    models: ModelData[],
    sections: Section[],
    toPrint: string[]
): void {
    for (const m of models) {
        // Check blend sections
        const blendSections = sections.filter(s =>
            s.lines.some(line =>
                hasKey(line, 'hash') && line.value.trim().toLowerCase() === m.refBlendHash.toLowerCase()
            )
        );

        const blendPatchedSections = blendSections.filter(s =>
            s.lines.some(line => hasKey(line, '$\\SRMI\\vertcount'))
        );

        if (blendPatchedSections.length > 0) {
            toPrint.push(`${blendPatchedSections[0].name} already patched with Pose Batch Fix. Skipping...`);
            m.blendConsumed = true;
        }

        // Check draw sections
        const drawSections = sections.filter(s =>
            s.lines.some(line =>
                hasKey(line, 'hash') && line.value.trim().toLowerCase() === m.refDrawHash.toLowerCase()
            )
        );

        const drawPatchedSections = drawSections.filter(s =>
            s.lines.some(line =>
                line.strippedLowerKey.includes('DRAW_TYPE != 8'.toLowerCase()) &&
                line.strippedLowerKey.includes('DRAW_TYPE != 1'.toLowerCase())
            )
        );

        if (drawPatchedSections.length > 0) {
            toPrint.push(`${drawPatchedSections[0].name} already patched with Pose Batch Fix. Skipping...`);
            m.drawConsumed = true;
        }

        // Check position resources
        const posResources = sections.filter(s =>
            m.posResource.name.trim().toLowerCase() === s.name.trim().toLowerCase().slice(1, -1)
        );

        if (posResources.length > 0) {
            toPrint.push(`${m.posResource.name} already exists in the INI file. Skipping resource creation...`);
            m.resConsumed = true;
        }
    }
}

function attemptMergeModBatchedPoseFix(
    sections: Section[],
    commandListCandidates: CommandListCandidate[],
    toPrint: string[],
    resTemplate: string,
    drawTemplate: string
): Section[] {
    const ifelTemplate = `{prev_block}
    if DRAW_TYPE == 8
        Resource\\SRMI\\PositionBuffer = ref {pos_res_name}
        Resource\\SRMI\\BlendBuffer = ref {blend_res_name}
        $\\SRMI\\vertcount = {vertcount}
    endif`;

    const blendMergeTemplate = `hash = {blend_hash}
run = {commandlist}
{draw_res} = copy {draw_res}
if DRAW_TYPE == 8
    Resource\\SRMI\\DrawBuffer = ref {draw_res}
elif DRAW_TYPE != 1
    $_blend_ = 2
endif
{rest_of_blend}`;

    const resTemplateSplit = resTemplate.split(/\r?\n/);
    const drawResTemplate = resTemplateSplit.slice(0, 5).join('\n');
    const blendPosResTemplate = resTemplateSplit.slice(5).join('\n');
    let resMergedStr = '\n[Constants]\nglobal $_blend_\n\n';
    const drawIfelTemplate = drawTemplate.split(/\r?\n/).slice(2).join('\n');

    for (const cl of commandListCandidates) {
        if (cl.commandList === null) {
            toPrint.push(`${ERROR_STR} CommandList for ${cl.blendHash} is missing. Skipping part...`);
            continue;
        }

        const ifelBlocks = splitInIfElseBlocks(cl.commandList.lines);
        if (ifelBlocks.length <= 1) {
            toPrint.push(`${WARN_STR} ${cl.commandList.name.trim()} doesn't have if else blocks. Invalid merge mod. Aborting...`);
            continue;
        }

        let finalCl = '';
        let finalDraw = '';
        let posStride = 40;
        let maxVCount = 0;
        const drawName = `Resource${cl.drawHash}DrawCS`;

        for (let bi = 0; bi < ifelBlocks.length; bi++) {
            const block = ifelBlocks[bi];
            let vCount = 0;
            let posRes = '';
            let blendRes = '';

            for (const bLine of block) {
                if (keyStartsWith(bLine, 'vb0')) {
                    posRes = bLine.value.trim();
                } else if (keyStartsWith(bLine, 'vb2')) {
                    blendRes = bLine.value.trim();
                } else if (keyStartsWith(bLine, 'draw')) {
                    if (!bLine.value.includes(',')) {
                        continue;
                    }
                    vCount = parseInt(bLine.value.split(',')[0]);
                    maxVCount = Math.max(maxVCount, vCount);
                }
            }

            if (posRes === '' || blendRes === '' || vCount === 0) {
                if (ifelBlocks.length - 1 === bi) {
                    finalCl += reconstructINIFile([createSection('', block, true)]) + '\n';
                    continue;
                }
                toPrint.push(`${ERROR_STR} Missing resource values in ${cl.commandList.name.trim()}. Skipping part...`);
                continue;
            }

            const ifelStr = ifelTemplate
                .replace('{prev_block}', reconstructINIFile([createSection('', block, true)]))
                .replace('{pos_res_name}', posRes + 'CS')
                .replace('{blend_res_name}', blendRes + 'CS')
                .replace('{vertcount}', vCount.toString());

            finalCl += ifelStr + '\n';

            try {
                const posResSection = sections.find(s =>
                    posRes.toLowerCase().trim() === s.name.toLowerCase().trim().slice(1).replace(/]$/, '')
                );
                const blendResSection = sections.find(s =>
                    blendRes.toLowerCase().trim() === s.name.toLowerCase().trim().slice(1).replace(/]$/, '')
                );

                if (!posResSection || !blendResSection) {
                    toPrint.push(`${ERROR_STR} Missing resource sections for ${cl.commandList.name.trim()}. Skipping part...`);
                    continue;
                }

                const [posName, posStrideVal, posFile] = getResourceData(posResSection);
                const [blendName, blendStrideVal, blendFile] = getResourceData(blendResSection);
                posStride = posStrideVal;

                resMergedStr += '\n' + blendPosResTemplate
                    .replace('{pos_name}', posName + 'CS')
                    .replace('{blend_name}', blendName + 'CS')
                    .replace('{pos_file}', posFile)
                    .replace('{blend_file}', blendFile)
                    .replace('{pos_stride}', posStride.toString())
                    .replace('{blend_stride}', blendStrideVal.toString()) + '\n';

                const firstLine = block[0];
                if (!(keyStartsWith(firstLine, 'if') ||
                    keyStartsWith(firstLine, 'else') ||
                    keyStartsWith(firstLine, 'elif'))) {
                    continue;
                }

                finalDraw += firstLine.key;
                finalDraw += drawIfelTemplate.replace('{draw_resource_name}', drawName) + '\n';
            } catch (error) {
                toPrint.push(`${ERROR_STR} Missing resource sections for ${cl.commandList.name.trim()}. Skipping part...`);
                continue;
            }
        }

        resMergedStr += drawResTemplate
            .replace('{draw_name}', drawName)
            .replace('{vertcount}', maxVCount.toString());

        // Patch CL
        for (const sec of sections) {
            if (sec.name.trim().toLowerCase() === cl.commandList.name.trim().toLowerCase()) {
                sec.lines.length = 0;
                addLines(sec, finalCl);
                clearEmptyEndingLines(sec);
                addSingleLine(sec, '\n');
                toPrint.push(`Patched ${sec.name.trim()} with Batched Pose Fix.`);
                break;
            }
        }

        // Patch DRAW section
        let drawSectionPatched = false;
        for (const sec of sections) {
            for (const line of sec.lines) {
                if (hasKey(line, 'hash') && line.value.trim().toLowerCase() === cl.drawHash) {
                    sec.lines.length = 0;
                    let finalDrawContent = `hash = ${cl.drawHash}\noverride_vertex_count = ${maxVCount}\noverride_byte_stride = ${posStride}\n${finalDraw}`;
                    if (finalDraw.startsWith('if')) {
                        finalDrawContent += 'endif\n';
                    }
                    addLines(sec, finalDrawContent);
                    toPrint.push(`Patched ${sec.name.trim()}'s DrawOverride with Batched Pose Fix.`);
                    cl.drawSectionPatched = true;
                    drawSectionPatched = true;
                    break;
                }
            }
            if (drawSectionPatched) break;
        }

        // Generate new draw section if not found
        if (!cl.drawSectionPatched) {
            for (const sec of sections) {
                for (const line of sec.lines) {
                    if (hasKey(line, 'hash') && line.strippedLowerValue === cl.blendHash) {
                        const newDrawSection = createSection(`[TextureOverride${cl.drawHash}Draw]\n`);
                        let finalDrawContent = `hash = ${cl.drawHash}\noverride_vertex_count = ${maxVCount}\noverride_byte_stride = ${posStride}\n${finalDraw}`;
                        if (finalDraw.startsWith('if')) {
                            finalDrawContent += 'endif\n';
                        }
                        addLines(newDrawSection, finalDrawContent);
                        sections.splice(sections.indexOf(sec) + 1, 0, newDrawSection);
                        toPrint.push(`Generated ${sec.name.trim()}'s DrawOverride with Batched Pose Fix applied.`);
                        cl.drawSectionPatched = true;
                        break;
                    }
                }
                if (cl.drawSectionPatched) break;
            }
        }

        // Patch blend section
        for (const sec of sections) {
            let restOfBlend = '';
            let blendFound = false;

            for (const line of sec.lines) {
                if (hasKey(line, 'hash') && line.strippedLowerValue === cl.blendHash) {
                    blendFound = true;
                } else if (hasKey(line, 'run') && line.strippedLowerValue === cl.commandList!.name.trim().slice(1).replace(/]$/, '').toLowerCase()) {
                    continue;
                } else {
                    restOfBlend += line.isValuePair ? `${line.key}=${line.value}` : line.key;
                }
            }

            if (blendFound) {
                const blendMergedStr = blendMergeTemplate
                    .replace('{blend_hash}', cl.blendHash)
                    .replace('{commandlist}', cl.commandList!.name.trim().slice(1).replace(/]$/, ''))
                    .replace(/\{draw_res\}/g, drawName)
                    .replace('{rest_of_blend}', restOfBlend);

                sec.lines.length = 0;
                addLines(sec, blendMergedStr);
                toPrint.push(`Patched ${sec.name.trim()} with hash ${cl.blendHash} with Batched Pose Fix.`);
                break;
            }
        }
    }

    const appendixSection = createSection('', [], true);
    addLines(appendixSection, resMergedStr);
    sections.push(appendixSection);
    return sections;
}

function isModPosePatched(sections: Section[]): boolean {
    for (const section of sections) {
        for (const line of section.lines) {
            if (hasKey(line, '$\\SRMI\\vertcount')) {
                return true;
            }
        }
    }
    return false;
}

function batchedPoseFix(
    filePath: string,
    sections: Section[],
    toPrint: string[]
): string {
    if (isModPosePatched(sections)) {
        toPrint.push('File already has Batched Pose Fix applied. Skipping...');
        return reconstructINIFile(sections);
    }

    const blendTemplate = `{draw_res_name} = copy {draw_res_name}
if DRAW_TYPE == 8    
    Resource\\SRMI\\PositionBuffer = ref {pos_res_name}
    Resource\\SRMI\\BlendBuffer = ref {blend_res_name}
    Resource\\SRMI\\DrawBuffer = ref {draw_res_name}
    $\\SRMI\\vertcount = {vertcount}
elif DRAW_TYPE != 1
    $_blend_ = 2
endif`;

    const drawTemplate = `override_vertex_count = {vertcount}
override_byte_stride = {byte_stride}
if DRAW_TYPE != 8 && DRAW_TYPE != 1 && $_blend_ > 0
    $_blend_ = $_blend_ - 1
    this = ref {draw_resource_name}
endif`;

    const resTemplate = `[{draw_name}]
type = RWStructuredBuffer
array = {vertcount}
data = R32_FLOAT 1 2 3 4 5 6 7 8 9 10

[{pos_name}]
type = StructuredBuffer
stride = {pos_stride}
filename = {pos_file}

[{blend_name}]
type = StructuredBuffer
stride = {blend_stride}
filename = {blend_file}
`;

    const blendList = Object.values(VALID_HASH_TRIOS).map(([b, _d, _p]) => b);
    const blendSections = sections.filter(s =>
        s.lines.some(line =>
            hasKey(line, 'hash') && blendList.includes(line.strippedLowerValue)
        )
    );

    if (blendSections.length === 0) {
        toPrint.push('File doesn\'t contain any blend override that needs batched pose patching. Skipping...');
        return reconstructINIFile(sections);
    }

    const commandListCandidates: CommandListCandidate[] = [];

    for (let i = 0; i < blendSections.length; i++) {
        const bSection = blendSections[i];
        commandListCandidates.push(createCommandListCandidate());

        for (const line of bSection.lines) {
            if (hasKey(line, 'hash')) {
                const matchingTrio = Object.values(VALID_HASH_TRIOS)
                    .find(([b, _d, _p]) => b === line.strippedLowerValue);
                if (matchingTrio) {
                    commandListCandidates[i].drawHash = matchingTrio[1];
                    commandListCandidates[i].blendHash = line.strippedLowerValue;
                }
            } else if (hasKey(line, 'vb0')) {
                commandListCandidates[i].hasVb0 = true;
            } else if (hasKey(line, 'run') && line.value.trim().toLowerCase().startsWith('commandlist')) {
                const tempName = line.value.trim();
                const foundCommandList = sections.find(s => hasName(s, tempName));
                commandListCandidates[i].commandList = foundCommandList || null;
            }
        }
    }

    // Check if all sections have commandlist and no vb0
    const allHaveCommandListNoVb0 = commandListCandidates.every(cl =>
        !cl.hasVb0 && cl.commandList !== null
    );

    if (allHaveCommandListNoVb0) {
        // We're in merge mod - verify CL integrity
        attemptMergeModBatchedPoseFix(sections, commandListCandidates, toPrint, resTemplate, drawTemplate);
        return reconstructINIFile(sections);
    }

    // Normal mod processing
    const models = gatherModelData(sections, blendSections, toPrint);

    // Check if ini needs patching or already has it
    checkModelData(models, sections, toPrint);

    let resourcesData = '';
    for (const m of models) {
        const drawResName = `Resource${m.partName || m.refDrawHash}DrawCS`;
        if (!m.resConsumed) {
            resourcesData += resTemplate
                .replace('{draw_name}', drawResName)
                .replace('{pos_name}', m.posResource.name)
                .replace('{blend_name}', m.blendResource.name)
                .replace('{vertcount}', m.vertcount.toString())
                .replace('{pos_stride}', m.posResource.stride.toString())
                .replace('{blend_stride}', m.blendResource.stride.toString())
                .replace('{pos_file}', m.posResource.filename)
                .replace('{blend_file}', m.blendResource.filename);
            m.resConsumed = true;
        }
    }

    for (const section of sections) {
        if (section.isHeader || !section.name.trim().toLowerCase().startsWith('[textureoverride')) {
            continue;
        }

        let sectionOverrideStride = 40;
        let sectionHash = '';
        const toPop: number[] = [];

        for (let j = 0; j < section.lines.length; j++) {
            const line = section.lines[j];
            if (hasKey(line, 'hash')) {
                sectionHash = line.value.trim().toLowerCase();
            } else if (hasKey(line, 'override_byte_stride')) {
                sectionOverrideStride = parseInt(line.value.trim());
                toPop.push(j);
            } else if (hasKey(line, 'override_vertex_count')) {
                toPop.push(j);
            }
        }

        // Remove lines in reverse order to maintain indices
        for (let j = toPop.length - 1; j >= 0; j--) {
            section.lines.splice(toPop[j], 1);
        }

        if (sectionHash === '') {
            toPrint.push(`${WARN_STR} Missing hash value in ${section.name.trim()}. Aborting...`);
            continue;
        }

        for (const m of models) {
            if (m.blendConsumed && m.drawConsumed && m.resConsumed) {
                continue;
            }

            const drawResName = `Resource${m.partName || m.refDrawHash}DrawCS`;

            if (m.refBlendHash === sectionHash && !m.blendConsumed) {
                const blendStr = blendTemplate
                    .replace('{vertcount}', m.vertcount.toString())
                    .replace('{pos_res_name}', m.posResource.name)
                    .replace('{blend_res_name}', m.blendResource.name)
                    .replace(/\{draw_res_name\}/g, drawResName);

                addLines(section, blendStr);
                toPrint.push(`${section.name.trim()} Batched Pose Fix applied to Blend Override!`);
                m.blendConsumed = true;
            } else if (m.refDrawHash === sectionHash && !m.drawConsumed) {
                if (sectionOverrideStride === 1) {
                    const posPath = path.join(path.dirname(filePath), m.posResource.filename);
                    if (!fs.existsSync(posPath)) {
                        toPrint.push(`${ERROR_STR} Missing resource file for ${section.name.trim()} and override_stride. Aborting...`);
                        continue;
                    }
                    const posSize = fs.statSync(posPath).size;
                    m.vertcount = Math.ceil(posSize / 40);
                }

                const drawMergedStr = drawTemplate
                    .replace('{vertcount}', m.vertcount.toString())
                    .replace('{byte_stride}', sectionOverrideStride.toString())
                    .replace('{draw_resource_name}', drawResName);

                addLines(section, drawMergedStr);
                toPrint.push(`${section.name.trim()} Batched Pose Fix applied to Draw Override!`);
                m.drawConsumed = true;
            }
        }
    }

    clearEmptyEndingLines(sections[sections.length - 1]);
    addSingleLine(sections[sections.length - 1], '\n');
    let finalINIBody = reconstructINIFile(sections);

    if (resourcesData) {
        toPrint.push('Resource sections added for Batch Pose Fix');
        resourcesData = '\n\n[Constants]\nglobal $_blend_ = 0\n\n; -------------------- Auto-generated CS resources --------------------\n\n' + resourcesData;
        const resSections = splitInSections(resourcesData);
        for (const section of resSections) {
            clearEmptyEndingLines(section);
            addSingleLine(section, '\n');
        }
        finalINIBody += reconstructINIFile(resSections);
    }

    return finalINIBody;
}

function cleanUpIndentation(content: string, _toPrint: string[]): string {
    const sections = splitInSections(content);

    for (const s of sections) {
        s.name = s.name.trimStart();
        let depth = 0;

        for (const line of s.lines) {
            if (line.strippedLowerKey === '') {
                continue;
            }

            if (keyStartsWith(line, 'if')) {
                depth++;
            } else if (keyStartsWith(line, 'endif')) {
                depth--;
            }

            if (keyStartsWith(line, 'if') || keyStartsWith(line, 'elif') || keyStartsWith(line, 'else')) {
                line.key = '\t'.repeat(depth - 1) + line.key.trimStart();
            } else {
                line.key = '\t'.repeat(depth) + line.key.trimStart();
            }
        }

        clearEmptyEndingLines(s);
        addSingleLine(s, '\n');
    }

    return reconstructINIFile(sections);
}

interface FixOptions {
    skipBatchedPose?: boolean;
    restoreBackups?: boolean;
}

class INIUpgrader {
    private content: string;
    private filepath: string;
    private hashes: string[];
    private touched: boolean;
    private doneHashes: Set<string>;

    constructor(filepath: string) {
        this.content = fs.readFileSync(filepath, 'utf8');
        this.filepath = filepath;
        this.hashes = [];
        this.touched = false;
        this.doneHashes = new Set();

        // Extract all hashes from the ini
        const hashPattern = /\s*hash\s*=\s*([A-Fa-f0-9]*)\s*/gi;
        const lines = this.content.split(/\r?\n/);

        for (const line of lines) {
            const match = hashPattern.exec(line);
            if (match) {
                this.hashes.push(match[1]);
            }
            hashPattern.lastIndex = 0; // Reset regex
        }
    }

    upgrade(options: FixOptions = {}): INIUpgrader {
        while (this.hashes.length > 0) {
            const hash = this.hashes.pop()!;
            if (!this.doneHashes.has(hash)) {
                if (hash in hashCommands) {
                    console.log(`\tUpgrading ${hash}`);
                    this.execute(hash, hashCommands[hash]);
                } else {
                    console.log(`\tSkipping ${hash}: - No upgrade available`);
                }
            } else {
                console.log(`\tSkipping ${hash}: / Already Checked/Upgraded`);
            }
            this.doneHashes.add(hash);
        }

        const toPrint: string[] = [];
        const newSections = posToBlendModdingFix(this.content, toPrint);

        let result: string;
        if (options.skipBatchedPose) {
            console.log('Skipping Batched Pose Fix');
            result = reconstructINIFile(newSections);
        } else {
            result = batchedPoseFix(this.filepath, newSections, toPrint);
        }

        if (this.content !== result) {
            this.touched = true;
            this.content = result;
        }

        this.content = cleanUpIndentation(this.content, toPrint);
        console.log('\t' + toPrint.join('\n\t'));

        return this;
    }

    private execute(hash: string, commands: Array<[string, any]>): void {
        for (const [command, kwargs] of commands) {
            if (command === 'info') {
                console.log(`\t- ${kwargs}`);
                continue;
            }

            if (command === 'upgrade_hash') {
                this.swapHash(hash, kwargs.to);
                this.hashes.push(kwargs.to);
            } else if (command === 'upgrade_shared_hash') {
                if (this.checkAnyHashesInINI(kwargs.flag_hashes)) {
                    console.log(`\t- ${kwargs.log_info}`);
                    this.swapHash(hash, kwargs.to);
                    this.hashes.push(kwargs.to);
                }
            }
            // Add more command implementations as needed
        }
    }

    private swapHash(oldHash: string, newHash: string): void {
        const hashPattern = new RegExp(`^\\s*hash\\s*=\\s*${oldHash}\\s*$`, 'gmi');
        const lines = this.content.split(/\r?\n/);
        const newLines: string[] = [];

        for (const line of lines) {
            if (hashPattern.test(line)) {
                newLines.push(`hash = ${newHash}`);
                newLines.push(`;${line}`);
                this.touched = true;
            } else {
                newLines.push(line);
            }
            hashPattern.lastIndex = 0;
        }

        this.content = newLines.join('\n');
    }

    private checkAnyHashesInINI(hashes: string[]): boolean {
        return hashes.some(h => this.hasHash(h));
    }

    private hasHash(hash: string): boolean {
        return this.hashes.includes(hash) || this.doneHashes.has(hash);
    }

    save(): void {
        if (this.touched) {
            const basename = path.basename(this.filepath).replace('.ini', '');
            const dirPath = path.dirname(this.filepath);
            const backupFilename = `DISABLED_BACKUP_${Math.floor(Date.now() / 1000)}.${basename}.ini`;
            const backupFullpath = path.join(dirPath, backupFilename);

            fs.renameSync(this.filepath, backupFullpath);
            console.log(`Created Backup: ${backupFilename} at ${dirPath}`);

            fs.writeFileSync(this.filepath, this.content, 'utf8');
            console.log('Updates applied');
        } else {
            console.log('No changes applied');
        }
        console.log();
    }
}

function upgradeINI(filepath: string, options: FixOptions = {}): boolean {
    try {
        const ini = new INIUpgrader(filepath).upgrade(options);
        ini.save();
        return true;
    } catch (error) {
        console.error(`Error occurred: ${error}`);
        console.error(`No changes have been applied to ${filepath}!`);
        console.error();
        return false;
    }
}

function processFolder(folderPath: string, options: FixOptions = {}): void {
    const entries = fs.readdirSync(folderPath, { withFileTypes: true });

    for (const entry of entries) {
        if (entry.name.toUpperCase().includes('DESKTOP')) {
            continue;
        }

        if (entry.name.toUpperCase().startsWith('DISABLED') && entry.name.endsWith('.ini')) {
            continue;
        }

        const filepath = path.join(folderPath, entry.name);

        if (entry.isDirectory()) {
            processFolder(filepath, options);
        } else if (entry.name.endsWith('.ini')) {
            console.log('Found .ini file:', filepath);
            if (options.restoreBackups) {
                restoreINI(filepath);
            } else {
                upgradeINI(filepath, options);
            }
        }
    }
}

function restoreINI(filepath: string): void {
    const basename = path.basename(filepath).replace('.ini', '');
    const dirPath = path.dirname(filepath);

    const candidates = fs.readdirSync(dirPath)
        .filter(f =>
            f.startsWith('DISABLED_BACKUP_') &&
            f.endsWith(`.${basename}.ini`) &&
            fs.statSync(path.join(dirPath, f)).isFile()
        );

    if (candidates.length === 0) {
        console.log(`\tNo backup found for ${filepath}. Skipping...`);
        return;
    }

    candidates.sort((a, b) => {
        const aTime = fs.statSync(path.join(dirPath, a)).mtime.getTime();
        const bTime = fs.statSync(path.join(dirPath, b)).mtime.getTime();
        return bTime - aTime; // Newest first
    });

    const backupFullpath = path.join(dirPath, candidates[0]);

    if (fs.existsSync(filepath)) {
        fs.unlinkSync(filepath);
        console.log(`\tRemoved: ${filepath}`);
    }

    fs.renameSync(backupFullpath, filepath);
    console.log(`\tRestored Backup: ${candidates[0]} at ${dirPath}`);
    console.log();
}

function directoryChecks(cwd: string): boolean {
    // Iterate back over path and find highest /Mods folder
    let cursorDir = path.resolve(cwd);
    let highestMods = cwd;
    let foundModsFolder = false;

    while (true) {
        const parentDir = path.dirname(cursorDir);
        if (parentDir === cursorDir) {
            // Reached root of drive, stop searching
            break;
        }

        if (path.basename(cursorDir).toLowerCase() === 'mods') {
            highestMods = cursorDir;
            foundModsFolder = true;
        }
        cursorDir = parentDir;
    }

    const corePath = path.join(path.dirname(highestMods), 'Core');

    if (!foundModsFolder) {
        console.log('You seem to be trying to run this script outside of a mods folder. Aborting...');
        return false;
    }

    if (!fs.existsSync(corePath)) {
        console.log(
            'XXMI install was not detected.\n' +
            'Please make sure you have XXMI installed properly.\n' +
            'This script will not work for the old SRMI, it has been deprecated and will no longer be supported.\n' +
            'Please migrate to the new XXMI Launcher then try using the script again.\n' +
            'XXMI migration Guide: https://leotorrez.github.io/modding/guides/getting-started'
        );
        return false;
    }

    return true;
}

/**
 * Main function to fix HSR mod files
 * @param modPath - Path to the mod file (.ini) or directory containing mod files
 * @param options - Optional configuration
 * @returns Promise<boolean> - Success status
 */
export async function fixHSRMod(
    modPath: string,
    options: FixOptions & {
        checkDirectory?: boolean;
        recursive?: boolean;
    } = {}
): Promise<boolean> {
    try {
        const resolvedPath = path.resolve(modPath);

        // Check if path exists
        if (!fs.existsSync(resolvedPath)) {
            console.error(`Path does not exist: ${resolvedPath}`);
            return false;
        }

        const stats = fs.statSync(resolvedPath);

        if (stats.isFile()) {
            // Handle single file
            if (!resolvedPath.endsWith('.ini')) {
                console.error('Passed file is not an INI file');
                return false;
            }

            console.log('Fixing single INI file:', resolvedPath);
            return upgradeINI(resolvedPath, options);
        } else if (stats.isDirectory()) {
            // Handle directory
            if (options.checkDirectory !== false) {
                if (!directoryChecks(resolvedPath)) {
                    return false;
                }
            }

            console.log(`Current working directory: ${resolvedPath}`);

            if (options.recursive !== false) {
                processFolder(resolvedPath, options);
            } else {
                // Process only files in the current directory
                const entries = fs.readdirSync(resolvedPath, { withFileTypes: true });

                for (const entry of entries) {
                    if (entry.name.toUpperCase().includes('DESKTOP')) {
                        continue;
                    }

                    if (entry.name.toUpperCase().startsWith('DISABLED') && entry.name.endsWith('.ini')) {
                        continue;
                    }

                    if (entry.isFile() && entry.name.endsWith('.ini')) {
                        const filepath = path.join(resolvedPath, entry.name);
                        console.log('Found .ini file:', filepath);

                        if (options.restoreBackups) {
                            restoreINI(filepath);
                        } else {
                            upgradeINI(filepath, options);
                        }
                    }
                }
            }

            console.log('Done!');
            return true;
        } else {
            console.error('Path is neither a file nor a directory');
            return false;
        }
    } catch (error) {
        console.error('Fatal error occurred:', error);
        return false;
    }
}

/**
 * Restore backups for HSR mod files
 * @param modPath - Path to the mod file (.ini) or directory containing mod files
 * @param recursive - Whether to process subdirectories recursively (default: true)
 * @returns Promise<boolean> - Success status
 */
export async function restoreHSRModBackups(
    modPath: string,
    recursive: boolean = true
): Promise<boolean> {
    return fixHSRMod(modPath, {
        restoreBackups: true,
        recursive,
        checkDirectory: false
    });
}

/**
 * Fix HSR mod with only hash updates (skip batched pose fix)
 * @param modPath - Path to the mod file (.ini) or directory containing mod files
 * @param recursive - Whether to process subdirectories recursively (default: true)
 * @returns Promise<boolean> - Success status
 */
export async function fixHSRModHashOnly(
    modPath: string,
    recursive: boolean = true
): Promise<boolean> {
    return fixHSRMod(modPath, {
        skipBatchedPose: true,
        recursive,
    });
}

// Default export for CommonJS compatibility
export default fixHSRMod;

// MARK: Hash commands
const hashCommands: Record<string, Array<[string, any]>> = {
    // MARK: Aglaea
    '119f3414': [['info', 'v3.1 -> v3.2: Aglaea Hair Draw Hash'], ['upgrade_hash', { 'to': '1e0c0119' }]],
    '417405f0': [
        ['upgrade_shared_hash', {
            'to': '4ee730fd',
            'flag_hashes': ['c7ad0566', '6ed5a76f', '1f0f1dc6', '457d09a4'],
            'log_info': 'v3.1 -> v3.2: Aglaea Head Draw Hash',
        }],
        ['upgrade_shared_hash', {
            'to': '4ee730fd',
            'flag_hashes': ['007cd17b', 'd802ea61', '08b089e7', 'a7f9383f'],
            'log_info': 'v3.1 -> v3.2: Gepard Head Draw Hash',
        }],
    ],
    '64b0dde3': [['info', 'v3.1 -> v3.2: Aglaea Body Draw Hash'], ['upgrade_hash', { 'to': '6b23e8ee' }]],


    // MARK: Argenti
    '099cb678': [['info', 'v1.6 -> v2.0: Argenti Body Texcoord Hash'], ['upgrade_hash', { 'to': '18af7e1c' }]],
    '9de080b0': [
        ['info', 'v1.6 -> v2.0: Argenti Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'ArgentiBody',
            'hash': '7d57f432',
            'trg_indices': ['0', '58749'],
            'src_indices': ['0', '-1'],
        }]
    ],

    '040c8f95': [['info', 'v2.2 -> v2.3: Argenti Hair Draw Hash'], ['upgrade_hash', { 'to': 'ac883ae6' }]],
    '3214c162': [['info', 'v2.2 -> v2.3: Argenti Hair Position Hash'], ['upgrade_hash', { 'to': '78c72ec8' }]],
    '5eede219': [['info', 'v2.2 -> v2.3: Argenti Hair Texcoord Hash'], ['upgrade_hash', { 'to': '05b75400' }]],
    '179d17fe': [['info', 'v2.2 -> v2.3: Argenti Hair IB Hash'], ['upgrade_hash', { 'to': '5fab0ace' }]],
    'd066d8b7': [['info', 'v2.2 -> v2.3: Argenti Hair Diffuse Hash'], ['upgrade_hash', { 'to': '17948e68' }]],
    '4925c9dd': [['info', 'v2.2 -> v2.3: Argenti Hair LightMap Hash'], ['upgrade_hash', { 'to': 'a13c6f7f' }]],

    '705196e4': [['info', 'v2.2 -> v2.3: Argenti Head Diffuse Hash'], ['upgrade_hash', { 'to': '2945bd23' }]],

    'f94c8a7e': [['info', 'v2.2 -> v2.3: Argenti Body Diffuse Hash'], ['upgrade_hash', { 'to': 'a4e4c7dc' }]],
    '98b6f3be': [['info', 'v2.2 -> v2.3: Argenti Body LightMap Hash'], ['upgrade_hash', { 'to': '63bb1f26' }]],

    '78c72ec8': [['info', 'v2.3 -> v3.1: Argenti Hair Position Hash'], ['upgrade_hash', { 'to': '6172337a' }]],
    '3f920a7c': [['info', 'v3.0 -> v3.1: Argenti Body Blend Hash'], ['upgrade_hash', { 'to': 'd0637428' }]],
    'cc1a18f7': [['info', 'v3.0 -> v3.1: Argenti Hair Blend Hash'], ['upgrade_hash', { 'to': '23eb66a3' }]],
    'e0caccfa': [['info', 'v3.0 -> v3.1: Argenti Head Blend Hash'], ['upgrade_hash', { 'to': '0f3bb2ae' }]],
    '13e52094': [['info', 'v3.0 -> v3.1: Argenti Head Position Hash'], ['upgrade_hash', { 'to': '0a503d26' }]],

    'ac883ae6': [['info', 'v2.3 -> v3.2: Argenti Hair Draw Hash'], ['upgrade_hash', { 'to': 'a31b0feb' }]],
    '0c349519': [['info', 'v3.1 -> v3.2: Argenti Head Draw Hash'], ['upgrade_hash', { 'to': '03a7a014' }]],
    '9dc7d1aa': [['info', 'v3.1 -> v3.2: Argenti Body Draw Hash'], ['upgrade_hash', { 'to': '9254e4a7' }]],
    '2e306dca': [['info', 'v3.1 -> v3.2: Argenti Body Position Hash'], ['upgrade_hash', { 'to': '8938505f' }]],



    // MARK: Arlan
    'efc1554c': [['info', 'v1.6 -> v2.0: Arlan BodyA LightMap Hash'], ['upgrade_hash', { 'to': '49f0a509' }]],
    'b83d39c9': [['info', 'v1.6 -> v2.0: Arlan BodyB LightMap Hash'], ['upgrade_hash', { 'to': 'ffaf499a' }]],
    '2b98f3d1': [['info', 'v1.6 -> v2.0: Arlan Body Texcoord Hash'], ['upgrade_hash', { 'to': '40436908' }]],
    'cb3a3965': [
        ['info', 'v1.6 -> v2.0: Arlan Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'ArlanBody',
            'hash': '31ebfc6e',
            'trg_indices': ['0', '23412', '41721', '42429'],
            'src_indices': ['0', '23412', '-1', '42429'],
        }]
    ],

    '21c2354a': [['info', 'v2.2 -> v2.3: Arlan Hair Diffuse Hash'], ['upgrade_hash', { 'to': '72ad2a8b' }]],
    '1fdfbbdc': [['info', 'v2.2 -> v2.3: Arlan Hair Lightmap Hash'], ['upgrade_hash', { 'to': 'b4c6e6a0' }]],

    '9a85af8a': [['info', 'v2.2 -> v2.3: Arlan Head Diffuse Hash'], ['upgrade_hash', { 'to': 'a8c57de3' }]],

    '52e4750b': [['info', 'v2.2 -> v2.3: Arlan BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '52b88238' }]],
    '49f0a509': [['info', 'v2.2 -> v2.3: Arlan BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'd8039952' }]],
    'd1e827e0': [['info', 'v2.2 -> v2.3: Arlan BodyB Diffuse Hash'], ['upgrade_hash', { 'to': 'f90343fb' }]],
    'ffaf499a': [['info', 'v2.2 -> v2.3: Arlan BodyB LightMap Hash'], ['upgrade_hash', { 'to': '2f5ce8b7' }]],

    'aa7d97fd': [['info', 'v3.0 -> v3.1: Arlan Body Blend Hash'], ['upgrade_hash', { 'to': '458ce9a9' }]],
    'b9ac47a5': [['info', 'v3.0 -> v3.1: Arlan Hair Blend Hash'], ['upgrade_hash', { 'to': '565d39f1' }]],
    '6aee5919': [['info', 'v3.0 -> v3.1: Arlan Hair Position Hash'], ['upgrade_hash', { 'to': '735b44ab' }]],
    '642d3ecb': [['info', 'v3.0 -> v3.1: Arlan Head Blend Hash'], ['upgrade_hash', { 'to': '8bdc409f' }]],
    '25060ff7': [['info', 'v3.0 -> v3.1: Arlan Head Position Hash'], ['upgrade_hash', { 'to': '3cb31245' }]],


    'adce6688': [['info', 'v3.1 -> v3.2: Arlan Hair Draw Hash'], ['upgrade_hash', { 'to': 'a25d5385' }]],
    '0a432b82': [['info', 'v3.1 -> v3.2: Arlan Head Draw Hash'], ['upgrade_hash', { 'to': '05d01e8f' }]],
    '8cc7a1d1': [['info', 'v3.1 -> v3.2: Arlan Body Draw Hash'], ['upgrade_hash', { 'to': '835494dc' }]],
    '3f301db1': [['info', 'v3.1 -> v3.2: Arlan Body Position Hash'], ['upgrade_hash', { 'to': 'b7db63bc' }]],


    // MARK: Asta
    '46c9c299': [['info', 'v1.6 -> v2.0: Asta Body Texcoord Hash'], ['upgrade_hash', { 'to': '337e94ce' }]],
    '099dd85b': [
        ['info', 'v1.6 -> v2.0: Asta Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'AstaBody',
            'hash': '8fb66ce1',
            'trg_indices': ['0', '4791', '11823', '12510', '47880'],
            'src_indices': ['0', '40161', '49917', '-1', '-1'],
        }]
    ],

    'cde8d751': [['info', 'v2.2 -> v2.3: Asta Hair Draw Hash'], ['upgrade_hash', { 'to': '1ca6cf3d' }]],
    '4e29dad2': [['info', 'v2.2 -> v2.3: Asta Hair Position Hash'], ['upgrade_hash', { 'to': '967c0759' }]],
    '6406c03e': [['info', 'v2.2 -> v2.3: Asta Hair Texcoord Hash'], ['upgrade_hash', { 'to': '4f796933' }]],
    '84668635': [['info', 'v2.2 -> v2.3: Asta Hair IB Hash'], ['upgrade_hash', { 'to': '36a13222' }]],
    '9bd1710d': [['info', 'v2.2 -> v2.3: Asta Hair Diffuse Hash'], ['upgrade_hash', { 'to': '2ec320aa' }]],
    '8206809f': [['info', 'v2.2 -> v2.3: Asta Hair LightMap Hash'], ['upgrade_hash', { 'to': '7fd9c40d' }]],

    '0fb34dc9': [['info', 'v2.2 -> v2.3: Asta Head Diffuse Hash'], ['upgrade_hash', { 'to': 'a53efe63' }]],

    'fb0f55f4': [['info', 'v2.2 -> v2.3: Asta BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'e290fff3' }]],
    '088765db': [['info', 'v2.2 -> v2.3: Asta BodyA LightMap Hash'], ['upgrade_hash', { 'to': '687428e3' }]],
    '3cc949c8': [['info', 'v2.2 -> v2.3: Asta BodyB Diffuse Hash'], ['upgrade_hash', { 'to': '8f61660a' }]],
    '701d9092': [['info', 'v2.2 -> v2.3: Asta BodyB LightMap Hash'], ['upgrade_hash', { 'to': '8893d921' }]],

    '967c0759': [['info', 'v2.3 -> v3.1: Asta Hair Position Hash'], ['upgrade_hash', { 'to': '8fc91aeb' }]],
    'f65d0287': [['info', 'v3.0 -> v3.1: Asta Body Blend Hash'], ['upgrade_hash', { 'to': '19ac7cd3' }]],
    'a6d492d3': [['info', 'v3.0 -> v3.1: Asta Hair Blend Hash'], ['upgrade_hash', { 'to': '4925ec87' }]],
    '8fd302a3': [['info', 'v3.0 -> v3.1: Asta Head Blend Hash'], ['upgrade_hash', { 'to': '60227cf7' }]],
    '6f2e00c4': [['info', 'v3.0 -> v3.1: Asta Head Position Hash'], ['upgrade_hash', { 'to': '769b1d76' }]],

    '1ca6cf3d': [['info', 'v2.3 -> v3.2: Asta Hair Draw Hash'], ['upgrade_hash', { 'to': '1335fa30' }]],
    '618d1c95': [['info', 'v3.1 -> v3.2: Asta Head Draw Hash'], ['upgrade_hash', { 'to': '6e1e2998' }]],
    '0b10db89': [['info', 'v3.1 -> v3.2: Asta Body Draw Hash'], ['upgrade_hash', { 'to': '0483ee84' }]],
    'b8e767e9': [['info', 'v3.1 -> v3.2: Asta Body Position Hash'], ['upgrade_hash', { 'to': '86253b68' }]],


    // MARK: Aventurine
    'c4c588df': [['info', 'v2.2 -> v2.3: Aventurine Hair Draw Hash'], ['upgrade_hash', { 'to': '2a1a1775' }]],
    '015c8a86': [['info', 'v2.2 -> v2.3: Aventurine Hair Position Hash'], ['upgrade_hash', { 'to': '8de65cb9' }]],
    '811fa2ca': [['info', 'v2.2 -> v2.3: Aventurine Hair Texcoord Hash'], ['upgrade_hash', { 'to': '32da43dd' }]],
    '015f4887': [['info', 'v2.2 -> v2.3: Aventurine Hair IB Hash'], ['upgrade_hash', { 'to': '59d6021b' }]],
    '7f4af1d5': [['info', 'v2.2 -> v2.3: Aventurine Hair Diffuse Hash'], ['upgrade_hash', { 'to': '7e21ce24' }]],
    '3bbbdfcc': [['info', 'v2.2 -> v2.3: Aventurine Hair LightMap Hash'], ['upgrade_hash', { 'to': '4699613b' }]],

    'c484fc3a': [['info', 'v2.2 -> v2.3: Aventurine Head Diffuse Hash'], ['upgrade_hash', { 'to': 'd4874355' }]],

    '982bd8c4': [['info', 'v2.2 -> v2.3: Aventurine Hair Texcoord Hash'], ['upgrade_hash', { 'to': '53bdb739' }]],
    '53c4098f': [['info', 'v2.2 -> v2.3: Aventurine Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'b1cd8482' }]],
    '6c801b21': [['info', 'v2.2 -> v2.3: Aventurine Hair LightMap Hash'], ['upgrade_hash', { 'to': '115d50a7' }]],

    '8de65cb9': [['info', 'v2.3 -> v3.1: Aventurine Hair Position Hash'], ['upgrade_hash', { 'to': '9453410b' }]],
    '8e11bb4f': [['info', 'v3.0 -> v3.1: Aventurine Body Blend Hash'], ['upgrade_hash', { 'to': '61e0c51b' }]],
    'dbef38cc': [['info', 'v3.0 -> v3.1: Aventurine Hair Blend Hash'], ['upgrade_hash', { 'to': '341e4698' }]],
    'd6703a6e': [['info', 'v3.0 -> v3.1: Aventurine Head Blend Hash'], ['upgrade_hash', { 'to': '3981443a' }]],
    'ad82dc30': [['info', 'v3.0 -> v3.1: Aventurine Head Position Hash'], ['upgrade_hash', { 'to': 'b437c182' }]],

    '2a1a1775': [['info', 'v2.3 -> v3.2: Aventurine Hair Draw Hash'], ['upgrade_hash', { 'to': '25892278' }]],
    '7df52768': [['info', 'v3.1 -> v3.2: Aventurine Head Draw Hash'], ['upgrade_hash', { 'to': '72661265' }]],
    'a9bd2aa3': [['info', 'v3.1 -> v3.2: Aventurine Body Draw Hash'], ['upgrade_hash', { 'to': 'a62e1fae' }]],
    '1a4a96c3': [['info', 'v3.1 -> v3.2: Aventurine Body Position Hash'], ['upgrade_hash', { 'to': '6bffa54d' }]],



    // MARK: Bailu
    'e5417fe2': [['info', 'v1.6 -> v2.0: Bailu Body Texcoord Hash'], ['upgrade_hash', { 'to': 'd7a8228a' }]],
    'dbf90364': [
        ['info', 'v1.6 -> v2.0: Bailu Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'BailuBody',
            'hash': '680253f0',
            'trg_indices': ['0', '33984', '56496', '62601'],
            'src_indices': ['0', '36429', '-1', '-1'],
        }]
    ],
    // '5dfaf99e': [
    //     ['info', 'v2.1: Bailu Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': 'e2fb7ce0'}],
    //     ['multiply_section', {
    //         'titles': ['BailuBodyPosition', 'BailuBodyPosition_Extra'],
    //         'hashes': ['5dfaf99e', 'e2fb7ce0']
    //     }]
    // ],

    'e2fb7ce0': [['info', 'v2.2 -> v3.1: Bailu Body Position Extra Hash'], ['comment_sections', {}]],
    'd1df61ab': [['info', 'v2.2 -> v2.3: Bailu Hair Diffuse Hash'], ['upgrade_hash', { 'to': '1a6134dc' }]],
    'dfe514d8': [['info', 'v2.2 -> v2.3: Bailu Hair LightMap Hash'], ['upgrade_hash', { 'to': 'dcc96667' }]],

    '52a50074': [['info', 'v2.2 -> v2.3: Bailu Head Diffuse Hash'], ['upgrade_hash', { 'to': '75770ba0' }]],

    'e3ea3823': [['info', 'v2.2 -> v2.3: Bailu BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'e430e059' }]],
    '74d8fa7a': [['info', 'v2.2 -> v2.3: Bailu BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'c42c0455' }]],
    'de6e235f': [['info', 'v2.2 -> v2.3: Bailu BodyB Diffuse Hash'], ['upgrade_hash', { 'to': 'e468513a' }]],
    'bdab2370': [['info', 'v2.2 -> v2.3: Bailu BodyB LightMap Hash'], ['upgrade_hash', { 'to': '8d372ffc' }]],

    '96a4a724': [['info', 'v3.0 -> v3.1: Bailu Body Blend Hash'], ['upgrade_hash', { 'to': '7955d970' }]],
    '68cef846': [['info', 'v3.0 -> v3.1: Bailu Hair Blend Hash'], ['upgrade_hash', { 'to': '873f8612' }]],
    '1779da3f': [['info', 'v3.0 -> v3.1: Bailu Hair Position Hash'], ['upgrade_hash', { 'to': '0eccc78d' }]],
    '15a034fc': [['info', 'v3.0 -> v3.1: Bailu Head Blend Hash'], ['upgrade_hash', { 'to': 'fa514aa8' }]],
    '0ae19e5d': [['info', 'v3.0 -> v3.1: Bailu Head Position Hash'], ['upgrade_hash', { 'to': '135483ef' }]],


    'a85facf8': [['info', 'v3.1 -> v3.2: Bailu Hair Draw Hash'], ['upgrade_hash', { 'to': 'a7cc99f5' }]],
    'cbf2493c': [['info', 'v3.1 -> v3.2: Bailu Head Draw Hash'], ['upgrade_hash', { 'to': 'c4617c31' }]],
    'ee0d45fe': [['info', 'v3.1 -> v3.2: Bailu Body Draw Hash'], ['upgrade_hash', { 'to': 'e19e70f3' }]],
    '5dfaf99e': [['info', 'v3.0 -> v3.1: Bailu Body Position Hash'], ['upgrade_hash', { 'to': 'fb4e6152' }]],

    // MARK: BlackSwan
    '96f25869': [['info', 'v2.0 -> v2.1: BlackSwan Body Texcoord Hash'], ['upgrade_hash', { 'to': '562fbdb4' }]],
    // '197e8353': [
    //     ['info', 'v2.1: BlackSwan Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '10fb3cab'}],
    //     ['multiply_section', {
    //         'titles': ['BlackSwanBodyPosition', 'BlackSwanBodyPosition_Extra'],
    //         'hashes': ['197e8353', '10fb3cab']
    //     }]
    // ],

    '10fb3cab': [['info', 'v2.2 -> v3.1: BlackSwan Body Position Extra Hash'], ['comment_sections', {}]],
    '5d782765': [['info', 'v2.2 -> v2.3: BlackSwan Hair Diffuse Hash'], ['upgrade_hash', { 'to': '9f71dd91' }]],
    '4013a662': [['info', 'v2.2 -> v2.3: BlackSwan Hair LightMap Hash'], ['upgrade_hash', { 'to': 'b97825d7' }]],

    '057dfd1a': [['info', 'v2.2 -> v2.3: BlackSwan Head Diffuse Hash'], ['upgrade_hash', { 'to': '7464fbfe' }]],

    '4ce38332': [['info', 'v2.2 -> v2.3: BlackSwan Body Diffuse Hash'], ['upgrade_hash', { 'to': 'a5727e55' }]],
    '5527e772': [['info', 'v2.2 -> v2.3: BlackSwan Body LightMap Hash'], ['upgrade_hash', { 'to': '7884691d' }]],
    '028b385d': [['info', 'v2.2 -> v2.3: BlackSwan Body StockingMap AMD Hash'], ['upgrade_hash', { 'to': 'ec1ba003' }]],
    '01f66a63': [['info', 'v2.2 -> v2.3: BlackSwan Body StockingMap NVDIA Hash'], ['upgrade_hash', { 'to': 'd037ddd6' }]],

    'b4ec029d': [['info', 'v3.0 -> v3.1: BlackSwan Body Blend Hash'], ['upgrade_hash', { 'to': '5b1d7cc9' }]],
    '0d8672ce': [['info', 'v3.0 -> v3.1: BlackSwan Hair Blend Hash'], ['upgrade_hash', { 'to': 'e2770c9a' }]],
    'dc153ce0': [['info', 'v3.0 -> v3.1: BlackSwan Hair Position Hash'], ['upgrade_hash', { 'to': 'c5a02152' }]],
    'ede8abb0': [['info', 'v3.0 -> v3.1: BlackSwan Head Blend Hash'], ['upgrade_hash', { 'to': '0219d5e4' }]],
    '33edc9b2': [['info', 'v3.0 -> v3.1: BlackSwan Head Position Hash'], ['upgrade_hash', { 'to': '2a58d400' }]],

    '197e8353': [['info', 'v3.0 -> v3.1: BlackSwan Body Position Hash'], ['upgrade_hash', { 'to': '094e2119' }]],
    'dda5d076': [['info', 'v3.1 -> v3.2: BlackSwan Hair Draw Hash'], ['upgrade_hash', { 'to': 'd236e57b' }]],
    '755e1d94': [['info', 'v3.1 -> v3.2: BlackSwan Head Draw Hash'], ['upgrade_hash', { 'to': '7acd2899' }]],
    'aa893f33': [['info', 'v3.1 -> v3.2: BlackSwan Body Draw Hash'], ['upgrade_hash', { 'to': 'a51a0a3e' }]],


    // MARK: Blade
    'b95b80ad': [['info', 'v1.5 -> v1.6: Blade BodyA LightMap Hash'], ['upgrade_hash', { 'to': '459ea4f3' }]],
    '0b7675c2': [['info', 'v1.5 -> v1.6: Blade BodyB LightMap Hash'], ['upgrade_hash', { 'to': 'bdbde74c' }]],

    // This is reverted in 2.3? Extremely weird, investigate later
    // '90237dd2': [['info', 'v1.6 -> v2.0: Blade Head Position Hash'],  ['upgrade_hash', {'to': '9bc595ba'}]],

    'b931dfc7': [['info', 'v1.6 -> v2.0: Blade Body Texcoord Hash'], ['upgrade_hash', { 'to': 'f7896b3e' }]],
    '5d03ae61': [
        ['info', 'v1.6 -> v2.0: Blade Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'BladeBody',
            'hash': '0eb1e389',
            'trg_indices': ['0', '35790', '44814'],
            'src_indices': ['0', '35790', '-1'],
        }]
    ],

    '419db05a': [['info', 'v2.2 -> v2.3: Blade Hair Draw Hash'], ['upgrade_hash', { 'to': '89af9f25' }]],
    '71b698d8': [['info', 'v2.2 -> v2.3: Blade Hair Position Hash'], ['upgrade_hash', { 'to': 'dd309961' }]],
    'ff18d193': [['info', 'v2.2 -> v2.3: Blade Hair Texcoord Hash'], ['upgrade_hash', { 'to': 'f646a974' }]],
    '60d6a2c4': [['info', 'v2.2 -> v2.3: Blade Hair IB Hash'], ['upgrade_hash', { 'to': 'ab8b5a42' }]],
    '7e354cb4': [['info', 'v2.2 -> v2.3: Blade Hair Diffuse Hash'], ['upgrade_hash', { 'to': '7cbac9fe' }]],
    '32919d62': [['info', 'v2.2 -> v2.3: Blade Hair LightMap Hash'], ['upgrade_hash', { 'to': 'bc05281a' }]],

    '9bc595ba': [['info', 'v2.2 -> v2.3: Blade Head Position Hash'], ['upgrade_hash', { 'to': '90237dd2' }]],
    '6fa7fbdc': [['info', 'v2.2 -> v2.3: Blade Head Diffuse Hash'], ['upgrade_hash', { 'to': '929dfaee' }]],

    '1082d394': [['info', 'v2.2 -> v2.3: Blade BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '6166ea57' }]],
    '459ea4f3': [['info', 'v2.2 -> v2.3: Blade BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'a273cfa3' }]],
    '409cd5c1': [['info', 'v2.2 -> v2.3: Blade BodyB Diffuse Hash'], ['upgrade_hash', { 'to': '3a1b9bb1' }]],
    'bdbde74c': [['info', 'v2.2 -> v2.3: Blade BodyB LightMap Hash'], ['upgrade_hash', { 'to': '647809bd' }]],

    '4cc66b74': [['info', 'v3.0 -> v3.1: Blade Body Blend Hash'], ['upgrade_hash', { 'to': 'a3371520' }]],
    'aab6366e': [['info', 'v3.0 -> v3.1: Blade Hair Blend Hash'], ['upgrade_hash', { 'to': '4547483a' }]],
    'dd309961': [['info', 'v2.3 -> v3.1: Blade Hair Position Hash'], ['upgrade_hash', { 'to': 'c48584d3' }]],
    '2bc042b8': [['info', 'v3.0 -> v3.1: Blade Head Blend Hash'], ['upgrade_hash', { 'to': 'c4313cec' }]],
    '90237dd2': [['info', 'v2.3 -> v3.1: Blade Head Position Hash'], ['upgrade_hash', { 'to': '89966060' }]],

    '89af9f25': [['info', 'v2.3 -> v3.2: Blade Hair Draw Hash'], ['upgrade_hash', { 'to': '863caa28' }]],
    '485280e8': [['info', 'v3.1 -> v3.2: Blade Head Draw Hash'], ['upgrade_hash', { 'to': '47c1b5e5' }]],
    '553ae2f8': [['info', 'v3.1 -> v3.2: Blade Body Draw Hash'], ['upgrade_hash', { 'to': '5aa9d7f5' }]],
    'e6cd5e98': [['info', 'v3.1 -> v3.2: Blade Body Position Hash'], ['upgrade_hash', { 'to': 'cd89693e' }]],



    // MARK: Boothill
    '1e9505b5': [['info', 'v2.2 -> v2.3: Boothill Hair Diffuse Hash'], ['upgrade_hash', { 'to': '3b420073' }]],
    '8dccfaa1': [['info', 'v2.2 -> v2.3: Boothill Hair LightMap Hash'], ['upgrade_hash', { 'to': 'af56a76b' }]],

    '4e49ef76': [['info', 'v2.2 -> v2.3: Boothill Head Diffuse Hash'], ['upgrade_hash', { 'to': '704d65a9' }]],

    '845f6f6b': [['info', 'v2.2 -> v2.3: Boothill Draw Hash'], ['upgrade_hash', { 'to': 'f261312e' }]],
    '37a8d30b': [['info', 'v2.2 -> v2.3: Boothill Position Hash'], ['upgrade_hash', { 'to': '41968d4e' }]],
    'd0fb7df5': [['info', 'v2.2 -> v2.3: Boothill Texcoord Hash'], ['upgrade_hash', { 'to': 'f8dd7e43' }]],
    '87f245a6': [['info', 'v2.2 -> v2.3: Boothill IB Hash'], ['upgrade_hash', { 'to': '3c3ec92a' }]],
    '6d0a3848': [['info', 'v2.2 -> v2.3: Boothill BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'bd451832' }]],
    'f914a7fe': [['info', 'v2.2 -> v2.3: Boothill BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'f36e4a49' }]],

    '5183fce7': [['info', 'v3.0 -> v3.1: Boothill Body Blend Hash'], ['upgrade_hash', { 'to': 'be7282b3' }]],
    'bd451832': [['info', 'v2.3 -> v3.1: Boothill BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '86707f18' }]],
    'f36e4a49': [['info', 'v2.3 -> v3.1: Boothill BodyA LightMap Hash'], ['upgrade_hash', { 'to': '6226eb4c' }]],
    'add5290e': [['info', 'v3.0 -> v3.1: Boothill Hair Blend Hash'], ['upgrade_hash', { 'to': '4224575a' }]],
    'd164f7fd': [['info', 'v3.0 -> v3.1: Boothill Hair Position Hash'], ['upgrade_hash', { 'to': 'c8d1ea4f' }]],
    'e495cc7d': [['info', 'v3.0 -> v3.1: Boothill Head Blend Hash'], ['upgrade_hash', { 'to': '0b64b229' }]],
    '80a5d96e': [['info', 'v3.0 -> v3.1: Boothill Head Position Hash'], ['upgrade_hash', { 'to': '9910c4dc' }]],

    'f261312e': [['info', 'v2.3 -> v3.2: Boothill Body Draw Hash'], ['upgrade_hash', { 'to': 'fdf20423' }]],
    '41968d4e': [['info', 'v2.3 -> v3.2: Boothill Body Position Hash'], ['upgrade_hash', { 'to': '8dbe6204' }]],
    'caa64d35': [['info', 'v3.1 -> v3.2: Boothill Hair Draw Hash'], ['upgrade_hash', { 'to': 'c5357838' }]],
    '18e79418': [['info', 'v3.1 -> v3.2: Boothill Head Draw Hash'], ['upgrade_hash', { 'to': '1774a115' }]],


    // MARK: Bronya
    'f25b360a': [['info', 'v1.5 -> v1.6: Bronya BodyA LightMap Hash'], ['upgrade_hash', { 'to': '066f1a5a' }]],
    '6989bd40': [['info', 'v1.5 -> v1.6: Bronya BodyB LightMap Hash'], ['upgrade_hash', { 'to': '5161422e' }]],
    '7f5e24df': [['info', 'v1.6 -> v2.0: Bronya Hair Draw Hash'], ['upgrade_hash', { 'to': '4e327afb' }]],
    '8123eaff': [['info', 'v1.6 -> v2.0: Bronya Hair Position Hash'], ['upgrade_hash', { 'to': '4265a087' }]],
    'd6153000': [['info', 'v1.6 -> v2.0: Bronya Hair Texcoord Hash'], ['upgrade_hash', { 'to': '2ec44855' }]],
    '70fd4690': [['info', 'v1.6 -> v2.0: Bronya Hair IB Hash'], ['upgrade_hash', { 'to': '2d03d71b' }]],
    '39d9a850': [['info', 'v1.6 -> v2.0: Bronya Body Texcoord Hash'], ['upgrade_hash', { 'to': '0d67a9c3' }]],
    '1d057d1a': [
        ['info', 'v1.6 -> v2.0: Bronya Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'BronyaBody',
            'hash': '29d03f40',
            'trg_indices': ['0', '34431', '36345', '60423'],
            'src_indices': ['0', '-1', '36345', '-1'],
        }]
    ],
    // '198eb408': [
    //     ['info', 'v2.1: Bronya Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '08f2d6dd'}],
    //     ['multiply_section', {
    //         'titles': ['BronyaBodyPosition', 'BronyaBodyPosition_Extra'],
    //         'hashes': ['198eb408', '08f2d6dd']
    //     }]
    // ],

    '08f2d6dd': [['info', 'v2.2 -> v3.1: Bronya Body Position Extra Hash'], ['comment_sections', {}]],
    '79319861': [['info', 'v2.2 -> v2.3: Bronya Hair Diffuse Hash'], ['upgrade_hash', { 'to': '7e9a40be' }]],
    'c476c030': [['info', 'v2.2 -> v2.3: Bronya Hair LightMap Hash'], ['upgrade_hash', { 'to': 'af5183a6' }]],

    '901262ce': [['info', 'v2.2 -> v2.3: Bronya Head Diffuse Hash'], ['upgrade_hash', { 'to': 'eea06253' }]],

    '0b49e488': [['info', 'v2.2 -> v2.3: Bronya BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '3ed22aab' }]],
    '066f1a5a': [['info', 'v2.2 -> v2.3: Bronya BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'b1117be0' }]],
    'ac738042': [['info', 'v2.2 -> v2.3: Bronya BodyA StockingMap Hash'], ['upgrade_hash', { 'to': '45480a99' }]],
    'e1c9d15e': [['info', 'v2.2 -> v2.3: Bronya BodyC Diffuse Hash'], ['upgrade_hash', { 'to': 'da221a45' }]],
    '5161422e': [['info', 'v2.2 -> v2.3: Bronya BodyC LightMap Hash'], ['upgrade_hash', { 'to': '643fe76a' }]],
    '720783d5': [['info', 'v2.2 -> v2.3: Bronya BodyC StockingMap Hash'], ['upgrade_hash', { 'to': '789f1abf' }]],

    '09c90e7f': [['info', 'v3.0 -> v3.1: Bronya Body Blend Hash'], ['upgrade_hash', { 'to': 'e638702b' }]],
    'cd417d46': [['info', 'v3.0 -> v3.1: Bronya Hair Blend Hash'], ['upgrade_hash', { 'to': '22b00312' }]],
    '4265a087': [['info', 'v2.0 -> v3.1: Bronya Hair Position Hash'], ['upgrade_hash', { 'to': '5bd0bd35' }]],
    '314fb3a3': [['info', 'v3.0 -> v3.1: Bronya Head Blend Hash'], ['upgrade_hash', { 'to': 'debecdf7' }]],
    '9718281f': [['info', 'v3.0 -> v3.1: Bronya Head Position Hash'], ['upgrade_hash', { 'to': '8ead35ad' }]],

    '4e327afb': [['info', 'v2.0 -> v3.2: Bronya Hair Draw Hash'], ['upgrade_hash', { 'to': '41a14ff6' }]],
    '198eb408': [['info', 'v3.0 -> v3.1: Bronya Body Position Hash'], ['upgrade_hash', { 'to': '1147cb6f' }]],
    '204acd53': [['info', 'v3.1 -> v3.2: Bronya Head Draw Hash'], ['upgrade_hash', { 'to': '2fd9f85e' }]],
    'aa790868': [['info', 'v3.1 -> v3.2: Bronya Body Draw Hash'], ['upgrade_hash', { 'to': 'a5ea3d65' }]],


    // MARK: Clara
    '7365de7c': [['info', 'v1.6 -> v2.0: Clara Hair Draw Hash'], ['upgrade_hash', { 'to': 'bcfb045b' }]],
    '8c56882c': [['info', 'v1.6 -> v2.0: Clara Hair Position Hash'], ['upgrade_hash', { 'to': '486f6900' }]],
    '572f5b77': [['info', 'v1.6 -> v2.0: Clara Hair Texcoord Hash'], ['upgrade_hash', { 'to': '08caadac' }]],
    '58982bbd': [['info', 'v1.6 -> v2.0: Clara Hair IB Hash'], ['upgrade_hash', { 'to': '338bbeec' }]],
    'da981c17': [['info', 'v1.6 -> v2.0: Clara Body Draw Hash'], ['upgrade_hash', { 'to': '8c9c698e' }]],
    '696fa077': [['info', 'v1.6 -> v2.0: Clara Body Draw Hash'], ['upgrade_hash', { 'to': '3f6bd5ee' }]],
    '5dfa8761': [['info', 'v1.6 -> v2.0: Clara Body Texcoord Hash'], ['upgrade_hash', { 'to': 'a444344c' }]],
    'f92afebc': [
        ['info', 'v1.6 -> v2.0: Clara Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'ClaraBody',
            'hash': '4a58be98',
            'trg_indices': ['0', '2016', '19290', '50910'],
            'src_indices': ['0', '-1', '19293', '-1'],
        }]
    ],

    '4c5e718d': [['info', 'v2.2 -> v2.3: Clara Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'e730fbcc' }]],
    '7fe8d517': [['info', 'v2.2 -> v2.3: Clara Hair LightMap Hash'], ['upgrade_hash', { 'to': '4ecb33c7' }]],

    'b6ba0179': [['info', 'v2.2 -> v2.3: Clara Head Diffuse Hash'], ['upgrade_hash', { 'to': '64cd257f' }]],

    'af43bb7c': [['info', 'v2.2 -> v2.3: Clara BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '198363bb' }]],
    'ffd2f41b': [['info', 'v2.2 -> v2.3: Clara BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'd73982e5' }]],
    'ff7a7e5e': [['info', 'v2.2 -> v2.3: Clara BodyC Diffuse Hash'], ['upgrade_hash', { 'to': 'a646bdde' }]],
    '6c866716': [['info', 'v2.2 -> v2.3: Clara BodyC LightMap Hash'], ['upgrade_hash', { 'to': '6f4c03fe' }]],

    '486f6900': [['info', 'v2.0 -> v3.1: Clara Hair Position Hash'], ['upgrade_hash', { 'to': '51da74b2' }]],
    '89e485a2': [['info', 'v3.0 -> v3.1: Clara Body Blend Hash'], ['upgrade_hash', { 'to': '6615fbf6' }]],
    'd7130c52': [['info', 'v3.0 -> v3.1: Clara Hair Blend Hash'], ['upgrade_hash', { 'to': '38e27206' }]],
    '01c3ad70': [['info', 'v3.0 -> v3.1: Clara Head Blend Hash'], ['upgrade_hash', { 'to': 'ee32d324' }]],
    'd252d8ba': [['info', 'v3.0 -> v3.1: Clara Head Position Hash'], ['upgrade_hash', { 'to': 'cbe7c508' }]],


    'bcfb045b': [['info', 'v2.0 -> v3.2: Clara Hair Draw Hash'], ['upgrade_hash', { 'to': 'b3683156' }]],
    '8c9c698e': [['info', 'v2.0 -> v3.2: Clara Body Draw Hash'], ['upgrade_hash', { 'to': '830f5c83' }]],
    '3f6bd5ee': [['info', 'v2.0 -> v3.2: Clara Body Position Hash'], ['upgrade_hash', { 'to': 'f4ad6f23' }]],
    'c1db0d65': [['info', 'v3.1 -> v3.2: Clara Head Draw Hash'], ['upgrade_hash', { 'to': 'ce483868' }]],


    // MARK: DanHeng
    'de0264c6': [['info', 'v1.4 -> v1.6: DanHeng BodyA LightMap Hash'], ['upgrade_hash', { 'to': '5e3149d6' }]],
    'f01e58df': [['info', 'v1.6 -> v2.0: DanHeng Head Texcoord Hash'], ['upgrade_hash', { 'to': '0c5e8d34' }]],
    'ab30fd81': [['info', 'v1.6 -> v2.0: DanHeng Body Texcoord Hash'], ['upgrade_hash', { 'to': '8bdfb25d' }]],
    'f256d83c': [['info', 'v1.6 -> v2.0: DanHeng BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '95212661' }]],
    'be813760': [
        ['info', 'v1.6 -> v2.0: DanHeng Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'DanHengBody',
            'hash': '457b4223',
            'trg_indices': ['0', '49005'],
            'src_indices': ['0', '-1'],
        }]
    ],

    '02394eab': [['info', 'v2.2 -> v2.3: DanHeng Hair Diffuse Hash'], ['upgrade_hash', { 'to': '62604aad' }]],
    '98fd88ae': [['info', 'v2.2 -> v2.3: DanHeng Hair LightMap Hash'], ['upgrade_hash', { 'to': 'e4fd41ae' }]],

    '1e764817': [['info', 'v2.2 -> v2.3: DanHeng Head Diffuse Hash'], ['upgrade_hash', { 'to': '65a5afa5' }]],

    '95212661': [['info', 'v2.2 -> v2.3: DanHeng Body Diffuse Hash'], ['upgrade_hash', { 'to': '72b7f37b' }]],
    '5e3149d6': [['info', 'v2.2 -> v2.3: DanHeng Body LightMap Hash'], ['upgrade_hash', { 'to': '01999151' }]],
    '01999151': [['info', 'v2.7 -> v3.0: DanHeng Body LightMap Hash'], ['upgrade_hash', { 'to': '75400c84' }]],

    'b7594abd': [['info', 'v3.0 -> v3.1: DanHeng Body Blend Hash'], ['upgrade_hash', { 'to': '58a834e9' }]],
    '031da129': [['info', 'v3.0 -> v3.1: DanHeng Hair Blend Hash'], ['upgrade_hash', { 'to': 'ececdf7d' }]],
    'b591da57': [['info', 'v3.0 -> v3.1: DanHeng Hair Position Hash'], ['upgrade_hash', { 'to': 'ac24c7e5' }]],
    '5bfc6e67': [['info', 'v3.0 -> v3.1: DanHeng Head Blend Hash'], ['upgrade_hash', { 'to': 'b40d1033' }]],
    '8ed66c8a': [['info', 'v3.0 -> v3.1: DanHeng Head Position Hash'], ['upgrade_hash', { 'to': '97637138' }]],

    'acf975aa': [['info', 'v3.1 -> v3.2: DanHeng Hair Draw Hash'], ['upgrade_hash', { 'to': 'a36a40a7' }]],
    'cf9c7841': [['info', 'v3.1 -> v3.2: DanHeng Head Draw Hash'], ['upgrade_hash', { 'to': 'c00f4d4c' }]],
    '2c5a53ab': [['info', 'v3.1 -> v3.2: DanHeng Body Draw Hash'], ['upgrade_hash', { 'to': '23c966a6' }]],
    '9fadefcb': [['info', 'v3.1 -> v3.2: DanHeng Body Position Hash'], ['upgrade_hash', { 'to': '48aa5ec1' }]],

    // MARK: DanHengIL
    '9249f149': [['info', 'v1.4 -> v1.6: DanHengIL BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'ef65d29c' }]],
    '0ffb8233': [['info', 'v1.6 -> v2.0: DanHengIL Body Texcoord Hash'], ['upgrade_hash', { 'to': '0f8da6ba' }]],
    '1a7ee87c': [
        ['info', 'v1.6 -> v2.0: DanHengIL Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'DanHengILBody',
            'hash': '7cb75a5e',
            'trg_indices': ['0', '47133'],
            'src_indices': ['0', '-1'],
        }]
    ],

    '5f6f803e': [['info', 'v2.2 -> v2.3: DanHengIL Hair Diffuse Hash'], ['upgrade_hash', { 'to': '779e60a8' }]],
    'ec8baa47': [['info', 'v2.2 -> v2.3: DanHengIL Hair LightMap Hash'], ['upgrade_hash', { 'to': '41840f8a' }]],

    'd64ab9dc': [['info', 'v2.2 -> v2.3: DanHengIL Head Diffuse Hash'], ['upgrade_hash', { 'to': 'f1b129e2' }]],

    '85486705': [['info', 'v2.2 -> v2.3: DanHengIL Body Diffuse Hash'], ['upgrade_hash', { 'to': '9300840e' }]],
    'ef65d29c': [['info', 'v2.2 -> v2.3: DanHengIL Body LightMap Hash'], ['upgrade_hash', { 'to': 'b0660300' }]],

    '9cbdcefd': [['info', 'v3.0 -> v3.1: DanHengIL Body Blend Hash'], ['upgrade_hash', { 'to': '734cb0a9' }]],
    '97be0a41': [['info', 'v3.0 -> v3.1: DanHengIL Hair Blend Hash'], ['upgrade_hash', { 'to': '784f7415' }]],
    '09d4edc5': [['info', 'v3.0 -> v3.1: DanHengIL Hair Position Hash'], ['upgrade_hash', { 'to': '1061f077' }]],
    '4616d1e7': [['info', 'v3.0 -> v3.1: DanHengIL Head Blend Hash'], ['upgrade_hash', { 'to': 'a9e7afb3' }]],
    'c7751dba': [['info', 'v3.0 -> v3.1: DanHengIL Head Position Hash'], ['upgrade_hash', { 'to': 'dec00008' }]],
    'c0933fd1': [['info', 'v3.0 -> v3.1: DanHengIL Horns Draw Hash'], ['upgrade_hash', { 'to': 'd9262263' }]],

    '2f69b239': [['info', 'v3.1 -> v3.2: DanHengIL Hair Draw Hash'], ['upgrade_hash', { 'to': '20fa8734' }]],
    'e556ac29': [['info', 'v3.1 -> v3.2: DanHengIL Head Draw Hash'], ['upgrade_hash', { 'to': 'eac59924' }]],
    'dc21ac4d': [['info', 'v3.1 -> v3.2: DanHengIL Body Draw Hash'], ['upgrade_hash', { 'to': 'd3b29940' }]],
    '6fd6102d': [['info', 'v3.1 -> v3.2: DanHengIL Body Position Hash'], ['upgrade_hash', { 'to': '093e22f4' }]],


    // MARK: DrRatio
    'd1795906': [['info', 'v1.6 -> v2.0: DrRatio Hair Draw Hash'], ['upgrade_hash', { 'to': 'fbcffe5a' }]],
    '4d6e85c4': [['info', 'v1.6 -> v2.0: DrRatio Hair Position Hash'], ['upgrade_hash', { 'to': '5ca10450' }]],
    'a8c25bde': [['info', 'v1.6 -> v2.0: DrRatio Hair Texcoord Hash'], ['upgrade_hash', { 'to': '26a8f257' }]],
    'f205cf29': [['info', 'v1.6 -> v2.0: DrRatio Hair IB Hash'], ['upgrade_hash', { 'to': '76d7d3f3' }]],
    '70238f05': [['info', 'v1.6 -> v2.0: DrRatio Head Draw Hash'], ['upgrade_hash', { 'to': '9857f892' }]],
    '8dfb8014': [['info', 'v1.6 -> v2.0: DrRatio Head Position Hash'], ['upgrade_hash', { 'to': 'b88dc8c6' }]],
    '874d30a8': [['info', 'v1.6 -> v2.0: DrRatio Head Texcoord Hash'], ['upgrade_hash', { 'to': '91f740da' }]],
    'ad2be93d': [['info', 'v1.6 -> v2.0: DrRatio Head IB Hash'], ['upgrade_hash', { 'to': '82bc4a2d' }]],
    'dc2c9035': [['info', 'v1.6 -> v2.0: DrRatio Body Draw Hash'], ['upgrade_hash', { 'to': 'd5f71e0e' }]],
    '6fdb2c55': [['info', 'v1.6 -> v2.0: DrRatio Body Position Hash'], ['upgrade_hash', { 'to': '6600a26e' }]],
    '32ccb687': [['info', 'v1.6 -> v2.0: DrRatio Body Texcoord Hash'], ['upgrade_hash', { 'to': 'e6b81399' }]],
    '4a12ec28': [
        ['info', 'v1.6 -> v2.0: DrRatio Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'DrRatioBody',
            'hash': '37c47042',
            'trg_indices': ['0', '56361'],
            'src_indices': ['0', '-1'],
        }]
    ],

    'fbcffe5a': [['info', 'v2.2 -> v2.3: DrRatio Hair Draw Hash'], ['upgrade_hash', { 'to': 'b310931e' }]],
    '5ca10450': [['info', 'v2.2 -> v2.3: DrRatio Hair Position Hash'], ['upgrade_hash', { 'to': '7a9d0dac' }]],
    '26a8f257': [['info', 'v2.2 -> v2.3: DrRatio Hair Texcoord Hash'], ['upgrade_hash', { 'to': '650888fc' }]],
    '76d7d3f3': [['info', 'v2.2 -> v2.3: DrRatio Hair IB Hash'], ['upgrade_hash', { 'to': '0a520e04' }]],
    '013f4f5d': [['info', 'v2.2 -> v2.3: DrRatio Hair Diffuse Hash'], ['upgrade_hash', { 'to': '521b3d2d' }]],
    '8eccb31c': [['info', 'v2.2 -> v2.3: DrRatio Hair LightMap Hash'], ['upgrade_hash', { 'to': '5a50e9ba' }]],

    '29a331d7': [['info', 'v2.2 -> v2.3: DrRatio Head Diffuse Hash'], ['upgrade_hash', { 'to': '4c6a99ed' }]],

    'd8ae56ba': [['info', 'v2.2 -> v2.3: DrRatio Body Diffuse Hash'], ['upgrade_hash', { 'to': 'e80725f3' }]],
    '9fa75d99': [['info', 'v2.2 -> v2.3: DrRatio Body LightMap Hash'], ['upgrade_hash', { 'to': '4329d27b' }]],

    'b88dc8c6': [['info', 'v2.0 -> v3.1: DrRatio Head Position Hash'], ['upgrade_hash', { 'to': 'a138d574' }]],
    '7a9d0dac': [['info', 'v2.3 -> v3.1: DrRatio Hair Position Hash'], ['upgrade_hash', { 'to': '6328101e' }]],
    'ec519551': [['info', 'v3.0 -> v3.1: DrRatio Body Blend Hash'], ['upgrade_hash', { 'to': '03a0eb05' }]],
    '7d37a021': [['info', 'v3.0 -> v3.1: DrRatio Hair Blend Hash'], ['upgrade_hash', { 'to': '92c6de75' }]],
    '6e1dc670': [['info', 'v3.0 -> v3.1: DrRatio Head Blend Hash'], ['upgrade_hash', { 'to': '81ecb824' }]],

    '9857f892': [['info', 'v2.0 -> v3.2: DrRatio Head Draw Hash'], ['upgrade_hash', { 'to': '97c4cd9f' }]],
    'd5f71e0e': [['info', 'v2.0 -> v3.2: DrRatio Body Draw Hash'], ['upgrade_hash', { 'to': 'da642b03' }]],
    '6600a26e': [['info', 'v2.0 -> v3.2: DrRatio Body Position Hash'], ['upgrade_hash', { 'to': '053732e7' }]],
    'b310931e': [['info', 'v2.3 -> v3.2: DrRatio Hair Draw Hash'], ['upgrade_hash', { 'to': 'bc83a613' }]],


    // MARK: Feixiao
    // '1ef800bc': [
    //     ['info', 'v2.5: Feixiao Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '85d02e23'}],
    //     ['multiply_section', {
    //         'titles': ['FeixiaoBodyPosition', 'FeixiaoBodyPosition_Extra'],
    //         'hashes': ['1ef800bc', '85d02e23']
    //     }]
    // ],
    '85d02e23': [['info', 'v2.2 -> v3.1: Feixiao Body Position Extra Hash'], ['comment_sections', {}]],

    '704530bd': [['info', 'v3.0 -> v3.1: Feixiao Body Blend Hash'], ['upgrade_hash', { 'to': '9fb44ee9' }]],
    'b5994a59': [['info', 'v3.0 -> v3.1: Feixiao Hair Blend Hash'], ['upgrade_hash', { 'to': '5a68340d' }]],
    '33418ff8': [['info', 'v3.0 -> v3.1: Feixiao Hair Position Hash'], ['upgrade_hash', { 'to': '2af4924a' }]],
    '3e6aa1cc': [['info', 'v3.0 -> v3.1: Feixiao Head Blend Hash'], ['upgrade_hash', { 'to': 'd19bdf98' }]],
    'fbd97f64': [['info', 'v3.0 -> v3.1: Feixiao Head Position Hash'], ['upgrade_hash', { 'to': 'e26c62d6' }]],
    'e4943d34': [['info', 'v3.0 -> v3.1: Feixiao Mark Blend Hash'], ['upgrade_hash', { 'to': '0b654360' }]],
    '39641aa2': [['info', 'v3.0 -> v3.1: Feixiao Mark Position Hash'], ['upgrade_hash', { 'to': '20d10710' }]],

    '1ef800bc': [['info', 'v3.0 -> v3.1: Feixiao Body Position Hash'], ['upgrade_hash', { 'to': '9c653391' }]],
    '7a972d55': [['info', 'v3.1 -> v3.2: Feixiao Hair Draw Hash'], ['upgrade_hash', { 'to': '75041858' }]],
    '035b9700': [['info', 'v3.1 -> v3.2: Feixiao Head Draw Hash'], ['upgrade_hash', { 'to': '0cc8a20d' }]],
    'ad0fbcdc': [['info', 'v3.1 -> v3.2: Feixiao Body Draw Hash'], ['upgrade_hash', { 'to': 'a29c89d1' }]],

    'b78e9538': [['info', 'v3.1 -> v3.2: Feixiao Mark Draw Hash'], ['upgrade_hash', { 'to': 'b81da035' }]],


    // MARK: Firefly
    '81984c7b': [['info', 'v2.2 -> v2.3 [npc/playable]: Firefly Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'cc46e8e8' }]],
    'cc46e8e8': [['info', 'v2.7 -> v3.0: Firefly Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'e0eeaba2' }]],
    '2cc928b2': [['info', 'v2.2 -> v2.3 [npc/playable]: Firefly Hair LightMap Hash'], ['upgrade_hash', { 'to': '38ae656e' }]],
    '38ae656e': [['info', 'v2.7 -> v3.0: Firefly Hair LightMap Hash'], ['upgrade_hash', { 'to': '61303c45' }]],

    '9966e83e': [['info', 'v2.2 -> v2.3 [npc/playable]: Firefly Head Diffuse Hash'], ['upgrade_hash', { 'to': 'c61c087d' }]],

    '8330592e': [['info', 'v2.2 -> v2.3 [npc/playable]: Firefly Body Draw Hash'], ['upgrade_hash', { 'to': 'da829543' }]],
    '30c7e54e': [['info', 'v2.2 -> v2.3 [npc/playable]: Firefly Body Position Hash'], ['upgrade_hash', { 'to': '69752923' }]],
    '274d9c39': [['info', 'v2.2 -> v2.3 [npc/playable]: Firefly Body Texcoord Hash'], ['upgrade_hash', { 'to': 'f57c4e74' }]],
    '977bcde9': [
        ['info', 'v2.2 -> v2.3 [npc/playable]: Firefly Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'FireflyBody',
            'hash': '423c22f1',
            'trg_indices': ['0', '32547', '66561'],
            'src_indices': ['0', '32976', '66429'],
        }]
    ],
    'b5be8f4f': [['info', 'v2.2 -> v2.3 [npc/playable]: Firefly Body Diffuse Hash'], ['upgrade_hash', { 'to': '70c1071f' }]],
    '04ea14b2': [['info', 'v2.2 -> v2.3 [npc/playable]: Firefly Body LightMap Hash'], ['upgrade_hash', { 'to': '3f9e2b37' }]],

    '681937c7': [['info', 'v3.0 -> v3.1: Firefly Body Blend Hash'], ['upgrade_hash', { 'to': '87e84993' }]],
    '7981904e': [['info', 'v3.0 -> v3.1: Firefly Hair Blend Hash'], ['upgrade_hash', { 'to': '9670ee1a' }]],
    'd0ebca93': [['info', 'v3.0 -> v3.1: Firefly Hair Position Hash'], ['upgrade_hash', { 'to': 'c95ed721' }]],

    'b5ca90ce': [['info', 'v3.0 -> v3.1: Firefly Head Blend Hash'], ['upgrade_hash', { 'to': '5a3bee9a' }]],
    '60fa9067': [['info', 'v3.0 -> v3.1: Firefly Head Position Hash'], ['upgrade_hash', { 'to': '794f8dd5' }]],


    '17f203db': [['info', 'v3.1 -> v3.2: Firefly Hair Draw Hash'], ['upgrade_hash', { 'to': '186136d6' }]],
    '3c61efc2': [['info', 'v3.1 -> v3.2: Firefly Head Draw Hash'], ['upgrade_hash', { 'to': '33f2dacf' }]],
    'da829543': [['info', 'v2.3 -> v3.2: Firefly Body Draw Hash'], ['upgrade_hash', { 'to': 'd511a04e' }]],
    '69752923': [['info', 'v2.3 -> v3.2: Firefly Body Position Hash'], ['upgrade_hash', { 'to': 'f8fbf6ce' }]],

    '5a3bee9a': [
        ['info', 'v3.2: Firefly Face Blend: Duplicate blend section for ultimate'],
        ['check_hash_not_in_ini', { 'hash': '94a5b64e' }],
        ['multiply_section', {
            'titles': ['FireflyFaceBlend', 'FireflyFaceBlend_Extra'],
            'hashes': ['5a3bee9a', '94a5b64e']
        }]
    ],


    // MARK: Firefly SAM
    '602bb9eb': [['info', 'v2.7 -> v3.0: Firefly SAM Ult Diffuse Hash'], ['upgrade_hash', { 'to': '006c5936' }]],
    '20f1a341': [['info', 'v3.0 -> v3.1: Sam Body Blend Hash'], ['upgrade_hash', { 'to': 'cf00dd15' }]],
    '6799671e': [['info', 'v3.0 -> v3.1: Sam Body Position Hash'], ['upgrade_hash', { 'to': '7e2c7aac' }]],

    '3c383ed4': [['info', 'v3.1 -> v3.2: Sam Body Draw Hash'], ['upgrade_hash', { 'to': '33ab0bd9' }]],
    'd631258c': [['info', 'v3.1 -> v3.2: Sam Wings Draw Hash'], ['upgrade_hash', { 'to': 'd9a21081' }]],
    'ebaebdc2': [['info', 'v3.1 -> v3.2: Sam Wings Position Hash'], ['upgrade_hash', { 'to': 'f21ba070' }]],
    '50ee777c': [['info', 'v3.1 -> v3.2: Sam Wings Blend Hash'], ['upgrade_hash', { 'to': 'bf1f0928' }]],

    // MARK: Fugue

    '94f94174': [['info', 'v3.0 -> v3.1: Fugue Body Blend Hash'], ['upgrade_hash', { 'to': '7b083f20' }]],
    '04d0f9a0': [['info', 'v3.0 -> v3.1: Fugue Tail Blend Hash'], ['upgrade_hash', { 'to': 'eb2187f4' }]],
    '5332f9ed': [['info', 'v3.0 -> v3.1: Fugue Body Blend Hash'], ['upgrade_hash', { 'to': 'bcc387b9' }]],
    '6d651fcc': [['info', 'v3.0 -> v3.1: Fugue Face Blend Hash'], ['upgrade_hash', { 'to': '82946198' }]],

    'c0f48e5a': [['info', 'v3.1 -> v3.2: Fugue Body Draw Hash'], ['upgrade_hash', { 'to': 'cf67bb57' }]],
    'a69170f8': [['info', 'v3.1 -> v3.2: Fugue Tail Draw Hash'], ['upgrade_hash', { 'to': 'a90245f5' }]],
    'b36af760': [['info', 'v3.1 -> v3.2: Fugue Hair Draw Hash'], ['upgrade_hash', { 'to': 'bcf9c26d' }]],
    '1f5a3282': [['info', 'v3.1 -> v3.2: Fugue Head Draw Hash'], ['upgrade_hash', { 'to': '10c9078f' }]],


    // MARK: FuXuan
    '71906b4e': [['info', 'v1.6 -> v2.0: FuXuan Body Texcoord Hash'], ['upgrade_hash', { 'to': '45b0663d' }]],
    '7d77bdb5': [
        ['info', 'v1.6 -> v2.0: FuXuan Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'FuXuanBody',
            'hash': 'c230f24a',
            'trg_indices': ['0', '39018', '57636', '65415'],
            'src_indices': ['0', '46797', '-1', '-1'],
        }]
    ],

    '73b1fe83': [['info', 'v2.2 -> v2.3: FuXuan Hair Texcoord Hash'], ['upgrade_hash', { 'to': 'f498555d' }]],
    'df067d4d': [['info', 'v2.2 -> v2.3: FuXuan Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'afb05dab' }]],
    'dfc8fb64': [['info', 'v2.2 -> v2.3: FuXuan Hair LightMap Hash'], ['upgrade_hash', { 'to': 'd4b96cd1' }]],

    '0dd26508': [['info', 'v2.2 -> v2.3: FuXuan Head Diffuse Hash'], ['upgrade_hash', { 'to': '0bf30362' }]],

    '9e822610': [['info', 'v2.2 -> v2.3: FuXuan BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '6455fc0a' }]],
    '50b30274': [['info', 'v2.2 -> v2.3: FuXuan BodyA LightMap Hash'], ['upgrade_hash', { 'to': '4ba289bf' }]],
    '0172c74d': [['info', 'v2.2 -> v2.3: FuXuan BodyB Diffuse Hash'], ['upgrade_hash', { 'to': '09c78c66' }]],
    'd9171ad6': [['info', 'v2.2 -> v2.3: FuXuan BodyB LightMap Hash'], ['upgrade_hash', { 'to': 'ce81f2e6' }]],
    '02291372': [['info', 'v2.2 -> v2.3: FuXuan BodyB StockingMap Hash'], ['upgrade_hash', { 'to': 'c7b3e7bd' }]],

    '3f5bd667': [['info', 'v3.0 -> v3.1: FuXuan Body Blend Hash'], ['upgrade_hash', { 'to': 'd0aaa833' }]],
    '8f1c057c': [['info', 'v3.0 -> v3.1: FuXuan Hair Blend Hash'], ['upgrade_hash', { 'to': '60ed7b28' }]],
    '5f23a159': [['info', 'v3.0 -> v3.1: FuXuan Hair Position Hash'], ['upgrade_hash', { 'to': '4696bceb' }]],
    'b11ec441': [['info', 'v3.0 -> v3.1: FuXuan Head Blend Hash'], ['upgrade_hash', { 'to': '5eefba15' }]],
    'f8d8d92e': [['info', 'v3.0 -> v3.1: FuXuan Head Position Hash'], ['upgrade_hash', { 'to': 'e16dc49c' }]],

    '84ba05f1': [['info', 'v3.1 -> v3.2: FuXuan Hair Draw Hash'], ['upgrade_hash', { 'to': '8b2930fc' }]],
    'eb684772': [['info', 'v3.1 -> v3.2: FuXuan Head Draw Hash'], ['upgrade_hash', { 'to': 'e4fb727f' }]],
    '529a3934': [['info', 'v3.1 -> v3.2: FuXuan Body Draw Hash'], ['upgrade_hash', { 'to': '5d090c39' }]],
    'e16d8554': [['info', 'v3.1 -> v3.2: FuXuan Body Position Hash'], ['upgrade_hash', { 'to': '62bdf52f' }]],


    // MARK: Gallagher
    '3464c771': [['info', 'v2.2 -> v2.3: Gallagher Hair Draw Hash'], ['upgrade_hash', { 'to': '4ce0e733' }]],
    'e2a6c3dd': [['info', 'v2.2 -> v2.3: Gallagher Hair Position Hash'], ['upgrade_hash', { 'to': 'b0198c11' }]],
    '8a910c8c': [['info', 'v2.2 -> v2.3: Gallagher Hair Texcoord Hash'], ['upgrade_hash', { 'to': '9023270b' }]],
    'f5c82676': [['info', 'v2.2 -> v2.3: Gallagher Hair IB Hash'], ['upgrade_hash', { 'to': 'e9f3a740' }]],
    '8590504d': [['info', 'v2.2 -> v2.3: Gallagher Hair Diffuse Hash'], ['upgrade_hash', { 'to': '0adf3bf9' }]],
    '69d380ac': [['info', 'v2.2 -> v2.3: Gallagher Hair LightMap Hash'], ['upgrade_hash', { 'to': 'b1f5a889' }]],

    '6c2c7e1c': [['info', 'v2.2 -> v2.3: Gallagher Head Diffuse Hash'], ['upgrade_hash', { 'to': '81a00110' }]],

    '4902ec09': [['info', 'v2.2 -> v2.3: Gallagher Body Diffuse Hash'], ['upgrade_hash', { 'to': '585134a8' }]],
    '851877a3': [['info', 'v2.2 -> v2.3: Gallagher Body LightMap Hash'], ['upgrade_hash', { 'to': '39bf93ba' }]],

    'b0198c11': [['info', 'v2.3 -> v3.1: Gallagher Hair Position Hash'], ['upgrade_hash', { 'to': 'a9ac91a3' }]],
    'b8346c8b': [['info', 'v3.0 -> v3.1: Gallagher Body Blend Hash'], ['upgrade_hash', { 'to': '57c512df' }]],
    'd9d4ed61': [['info', 'v3.0 -> v3.1: Gallagher Hair Blend Hash'], ['upgrade_hash', { 'to': '36259335' }]],
    '0a7424b1': [['info', 'v3.0 -> v3.1: Gallagher Head Blend Hash'], ['upgrade_hash', { 'to': 'e5855ae5' }]],
    'ac642ccc': [['info', 'v3.0 -> v3.1: Gallagher Head Position Hash'], ['upgrade_hash', { 'to': 'b5d1317e' }]],

    '4ce0e733': [['info', 'v2.3 -> v3.2: Gallagher Hair Draw Hash'], ['upgrade_hash', { 'to': '4373d23e' }]],
    '0a3ab5fd': [['info', 'v3.1 -> v3.2: Gallagher Head Draw Hash'], ['upgrade_hash', { 'to': '05a980f0' }]],
    '623f8510': [['info', 'v3.1 -> v3.2: Gallagher Body Draw Hash'], ['upgrade_hash', { 'to': '6dacb01d' }]],
    'd1c83970': [['info', 'v3.1 -> v3.2: Gallagher Body Position Hash'], ['upgrade_hash', { 'to': '46f13636' }]],


    // MARK: Gepard
    'd62bbd0f': [['info', 'v1.6 -> v2.0: Gepard Body Texcoord Hash'], ['upgrade_hash', { 'to': '04094d7e' }]],
    '30aa99d6': [
        ['info', 'v1.6 -> v2.0: Gepard Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'GepardBody',
            'hash': '1e4f876c',
            'trg_indices': ['0', '27621', '55773', '57774'],
            'src_indices': ['0', '31266', '-1', '-1'],
        }]
    ],

    '71ba118e': [['info', 'v2.2 -> v2.3: Gepard Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'a4d9351f' }]],
    '12718dd9': [['info', 'v2.2 -> v2.3: Gepard Hair LightMap Hash'], ['upgrade_hash', { 'to': '00e5e932' }]],

    '67bf8ce8': [['info', 'v2.2 -> v2.3: Gepard Head Diffuse Hash'], ['upgrade_hash', { 'to': '32a6a2cc' }]],

    '19731fb9': [['info', 'v2.2 -> v2.3: Gepard BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'e70c5ef2' }]],
    'da172387': [['info', 'v2.2 -> v2.3: Gepard BodyA LightMap Hash'], ['upgrade_hash', { 'to': '2ca81203' }]],
    '369fb8ef': [['info', 'v2.2 -> v2.3: Gepard BodyB Diffuse Hash'], ['upgrade_hash', { 'to': 'aff5c287' }]],
    '2482636f': [['info', 'v2.2 -> v2.3: Gepard BodyB LightMap Hash'], ['upgrade_hash', { 'to': '2ba5e966' }]],

    '9875df96': [['info', 'v3.0 -> v3.1: Gepard Body Blend Hash'], ['upgrade_hash', { 'to': '7784a1c2' }]],
    'ff3a4705': [['info', 'v3.0 -> v3.1: Gepard Hair Blend Hash'], ['upgrade_hash', { 'to': '10cb3951' }]],
    '26734c62': [['info', 'v3.0 -> v3.1: Gepard Hair Position Hash'], ['upgrade_hash', { 'to': '3fc651d0' }]],
    '37f39435': [['info', 'v3.0 -> v3.1: Gepard Head Blend Hash'], ['upgrade_hash', { 'to': 'd802ea61' }]],
    '19c9ccc9': [['info', 'v3.0 -> v3.1: Gepard Head Position Hash'], ['upgrade_hash', { 'to': '007cd17b' }]],

    '0c9c901c': [['info', 'v3.1 -> v3.2: Gepard Hair Draw Hash'], ['upgrade_hash', { 'to': '030fa511' }]],

    '64e03e8e': [['info', 'v3.1 -> v3.2: Gepard Body Draw Hash'], ['upgrade_hash', { 'to': '6b730b83' }]],
    'd71782ee': [['info', 'v3.1 -> v3.2: Gepard Body Position Hash'], ['upgrade_hash', { 'to': '20cf413e' }]],

    // MARK: Guinaifen
    'de1f98c0': [['info', 'v1.6 -> v2.0: Guinaifen Body Draw Hash'], ['upgrade_hash', { 'to': '637ad2db' }]],
    '6de824a0': [['info', 'v1.6 -> v2.0: Guinaifen Body Position Hash'], ['upgrade_hash', { 'to': 'd08d6ebb' }]],
    '4b1cdcfc': [['info', 'v1.6 -> v2.0: Guinaifen Body Position Extra Hash'], ['upgrade_hash', { 'to': '506edd10' }]],
    '6e216a03': [['info', 'v1.6 -> v2.0: Guinaifen Body Texcoord Hash'], ['upgrade_hash', { 'to': '2eeff76f' }]],
    '75d5ec54': [
        ['info', 'v1.6 -> v2.0: Guinaifen Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'GuinaifenBody',
            'hash': '79900144',
            'trg_indices': ['0', '8907', '34146', '54723'],
            'src_indices': ['0', '-1', '34146', '-1'],
        }]
    ],
    // 'd08d6ebb': [
    //     ['info', 'v2.1: Guinaifen Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '506edd10'}],
    //     ['check_hash_not_in_ini', {'hash': '4b1cdcfc'}],
    //     ['multiply_section', {
    //         'titles': ['GuinaifenBodyPosition', 'GuinaifenBodyPosition_Extra'],
    //         'hashes': ['d08d6ebb', '506edd10']
    //     }]
    // ],

    'c88f1557': [['info', 'v2.2 -> v2.3: Guinaifen Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'fbd7db30' }]],
    '33043521': [['info', 'v2.2 -> v2.3: Guinaifen Hair LightMap Hash'], ['upgrade_hash', { 'to': 'c6e13e26' }]],

    '7c097e20': [['info', 'v2.2 -> v2.3: Guinaifen Head Diffuse Hash'], ['upgrade_hash', { 'to': '81dd54bc' }]],

    'e73b9426': [['info', 'v2.2 -> v2.3: Guinaifen BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'ae6de86c' }]],
    'd6a8cff9': [['info', 'v2.2 -> v2.3: Guinaifen BodyA LightMap Hash'], ['upgrade_hash', { 'to': '4092649e' }]],
    '47551426': [['info', 'v2.2 -> v2.3: Guinaifen BodyA StockingMap Hash'], ['upgrade_hash', { 'to': 'caf58d2a' }]],
    'd5d770b0': [['info', 'v2.2 -> v2.3: Guinaifen BodyC Diffuse Hash'], ['upgrade_hash', { 'to': 'b710c78e' }]],
    'a72e61d5': [['info', 'v2.2 -> v2.3: Guinaifen BodyC LightMap Hash'], ['upgrade_hash', { 'to': '4463cc21' }]],

    '637ad2db': [['info', 'v2.0 -> v3.1: Guinaifen Body Draw Hash'], ['upgrade_hash', { 'to': '6ce9e7d6' }]],
    'd08d6ebb': [['info', 'v3.0 -> v3.1: Guinaifen Body Position Hash'], ['upgrade_hash', { 'to': '49dbc0a2' }]],
    '506edd10': [['info', 'v2.0 -> v3.1: Guinaifen Body Position Extra Hash'], ['comment_sections', {}]],
    '93f9c6fc': [['info', 'v3.0 -> v3.1: Guinaifen Body Blend Hash'], ['upgrade_hash', { 'to': '7c08b8a8' }]],
    '7e75c06e': [['info', 'v3.0 -> v3.1: Guinaifen Hair Blend Hash'], ['upgrade_hash', { 'to': '9184be3a' }]],
    '1b4cc6bb': [['info', 'v3.0 -> v3.1: Guinaifen Hair Position Hash'], ['upgrade_hash', { 'to': '02f9db09' }]],
    '3753bbee': [['info', 'v3.0 -> v3.1: Guinaifen Head Blend Hash'], ['upgrade_hash', { 'to': 'd8a2c5ba' }]],
    '735df382': [['info', 'v3.0 -> v3.1: Guinaifen Head Position Hash'], ['upgrade_hash', { 'to': '6ae8ee30' }]],

    'f405349a': [['info', 'v3.1 -> v3.2: Guinaifen Hair Draw Hash'], ['upgrade_hash', { 'to': 'fb960197' }]],
    'b853ead4': [['info', 'v3.1 -> v3.2: Guinaifen Head Draw Hash'], ['upgrade_hash', { 'to': 'b7c0dfd9' }]],


    // MARK: Hanya
    'a73510da': [['info', 'v1.6 -> v2.0: Hanya Body Texcoord Hash'], ['upgrade_hash', { 'to': '69a81bdb' }]],
    '42de1256': [
        ['info', 'v1.6 -> v2.0: Hanya Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'HanyaBody',
            'hash': 'b1c2c937',
            'trg_indices': ['0', '28818', '51666', '52734'],
            'src_indices': ['0', '29886', '-1', '-1'],
        }]
    ],

    '8bc1d1db': [['info', 'v2.2 -> v2.3: Hanya Hair Diffuse Hash'], ['upgrade_hash', { 'to': '7b9e82c5' }]],
    '18503e31': [['info', 'v2.2 -> v2.3: Hanya Hair LightMap Hash'], ['upgrade_hash', { 'to': '44c3983d' }]],

    '19cae91f': [['info', 'v2.2 -> v2.3: Hanya Head Diffuse Hash'], ['upgrade_hash', { 'to': '6d95729a' }]],

    'b6dea863': [['info', 'v2.2 -> v2.3: Hanya BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '3a1da416' }]],
    'b4d0253c': [['info', 'v2.2 -> v2.3: Hanya BodyA LightMap Hash'], ['upgrade_hash', { 'to': '7c08d55d' }]],
    '9233c696': [['info', 'v2.2 -> v2.3: Hanya BodyA StockingMap Hash'], ['upgrade_hash', { 'to': '162667f6' }]],
    'e7afec9f': [['info', 'v2.2 -> v2.3: Hanya BodyB Diffuse Hash'], ['upgrade_hash', { 'to': 'd927b45a' }]],
    'c2817103': [['info', 'v2.2 -> v2.3: Hanya BodyB LightMap Hash'], ['upgrade_hash', { 'to': '537979fe' }]],
    'ca76ff40': [['info', 'v2.2 -> v2.3: Hanya BodyB StockingMap Hash'], ['upgrade_hash', { 'to': '61d0592b' }]],

    '208022e7': [['info', 'v3.0 -> v3.1: Hanya Body Blend Hash'], ['upgrade_hash', { 'to': 'cf715cb3' }]],
    'a15c444f': [['info', 'v3.0 -> v3.1: Hanya Hair Blend Hash'], ['upgrade_hash', { 'to': '4ead3a1b' }]],
    '10952bd7': [['info', 'v3.0 -> v3.1: Hanya Hair Position Hash'], ['upgrade_hash', { 'to': '09203665' }]],
    '5311cf0a': [['info', 'v3.0 -> v3.1: Hanya Head Blend Hash'], ['upgrade_hash', { 'to': 'bce0b15e' }]],
    'adf8b2de': [['info', 'v3.0 -> v3.1: Hanya Head Position Hash'], ['upgrade_hash', { 'to': 'b44daf6c' }]],

    'c6421b31': [['info', 'v3.1 -> v3.2: Hanya Hair Draw Hash'], ['upgrade_hash', { 'to': 'c9d12e3c' }]],
    'ae996433': [['info', 'v3.1 -> v3.2: Hanya Head Draw Hash'], ['upgrade_hash', { 'to': 'a10a513e' }]],
    'ceff860d': [['info', 'v3.1 -> v3.2: Hanya Body Draw Hash'], ['upgrade_hash', { 'to': 'c16cb300' }]],
    '7d083a6d': [['info', 'v3.1 -> v3.2: Hanya Body Position Hash'], ['upgrade_hash', { 'to': 'be3b14b5' }]],


    // MARK: Herta
    '93835e8f': [['info', 'v1.6 -> v2.0: Herta Body Draw Hash'], ['upgrade_hash', { 'to': 'c08327f8' }]],
    '2074e2ef': [['info', 'v1.6 -> v2.0: Herta Body Position Hash'], ['upgrade_hash', { 'to': '73749b98' }]],
    'c12363b4': [['info', 'v1.6 -> v2.0: Herta Body Texcoord Hash'], ['upgrade_hash', { 'to': '91c0cb8e' }]],
    '5186a9b8': [
        ['info', 'v1.6 -> v2.0: Herta Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'HertaBody',
            'hash': '9553ff35',
            'trg_indices': ['0', '8814', '53166'],
            'src_indices': ['0', '-1', '52458'],
        }]
    ],

    'd53e94bd': [['info', 'v2.2 -> v2.3: Herta Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'ee995067' }]],
    '84c9c04b': [['info', 'v2.2 -> v2.3: Herta Hair LightMap Hash'], ['upgrade_hash', { 'to': '515a7733' }]],

    '029aeabf': [['info', 'v2.2 -> v2.3: Herta Head Diffuse Hash'], ['upgrade_hash', { 'to': 'e116363f' }]],

    '01057b08': [['info', 'v2.2 -> v2.3: Herta Body Diffuse Hash'], ['upgrade_hash', { 'to': 'e07c10c9' }]],
    '22d89ecd': [['info', 'v2.2 -> v2.3: Herta Body LightMap Hash'], ['upgrade_hash', { 'to': 'b878ef55' }]],

    '383d8083': [['info', 'v3.0 -> v3.1: Herta Body Blend Hash'], ['upgrade_hash', { 'to': 'd7ccfed7' }]],
    '40ff8968': [['info', 'v3.0 -> v3.1: Herta Hair Blend Hash'], ['upgrade_hash', { 'to': 'af0ef73c' }]],
    '2d748a84': [['info', 'v3.0 -> v3.1: Herta Hair Position Hash'], ['upgrade_hash', { 'to': '34c19736' }]],
    'c1948160': [['info', 'v3.0 -> v3.1: Herta Head Blend Hash'], ['upgrade_hash', { 'to': '2e65ff34' }]],
    '93d98b8b': [['info', 'v3.0 -> v3.1: Herta Head Position Hash'], ['upgrade_hash', { 'to': '8a6c9639' }]],

    'c08327f8': [['info', 'v2.0 -> v3.2: Herta Body Draw Hash'], ['upgrade_hash', { 'to': 'cf1012f5' }]],
    '73749b98': [['info', 'v2.0 -> v3.2: Herta Body Position Hash'], ['upgrade_hash', { 'to': '6b999ed4' }]],
    'f9eda56d': [['info', 'v3.1 -> v3.2: Herta Hair Draw Hash'], ['upgrade_hash', { 'to': 'f67e9060' }]],
    '011f3657': [['info', 'v3.1 -> v3.2: Herta Head Draw Hash'], ['upgrade_hash', { 'to': '0e8c035a' }]],


    // MARK: Himeko
    '5d98de11': [['info', 'v1.6 -> v2.0: Himeko Body Position Extra Hash'], ['upgrade_hash', { 'to': '3cfb3645' }]],
    '77cb214c': [['info', 'v1.6 -> v2.0: Himeko Body Texcoord Hash'], ['upgrade_hash', { 'to': 'b9e9ae3b' }]],
    'e4640c8c': [
        ['info', 'v1.6 -> v2.0: Himeko Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'HimekoBody',
            'hash': 'e79e4018',
            'trg_indices': ['0', '27381', '37002', '47634'],
            'src_indices': ['-1', '0', '37002', '-1'],
        }]
    ],

    'c08f4727': [['info', 'v2.2 -> v2.3: Himeko Hair Texcoord Hash'], ['upgrade_hash', { 'to': 'fa440b40' }]],
    'fc068361': [['info', 'v2.2 -> v2.3: Himeko Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'd4634d6f' }]],
    '9adcae2d': [['info', 'v2.2 -> v2.3: Himeko Hair LightMap Hash'], ['upgrade_hash', { 'to': 'a700d6b4' }]],

    '1acfc83f': [['info', 'v2.2 -> v2.3: Himeko Head Diffuse Hash'], ['upgrade_hash', { 'to': '832e3b54' }]],

    'f4b0bd6d': [['info', 'v2.2 -> v2.3: Himeko Body Draw Hash'], ['upgrade_hash', { 'to': '62d53b1f' }]],
    '4747010d': [['info', 'v2.2 -> v2.3: Himeko Body Position Hash'], ['upgrade_hash', { 'to': 'd122877f' }]],
    'b9e9ae3b': [['info', 'v2.2 -> v2.3: Himeko Body Texcoord Hash'], ['upgrade_hash', { 'to': '2bf29f1f' }]],
    'e79e4018': [['info', 'v2.2 -> v2.3: Himeko Body IB Hash'], ['upgrade_hash', { 'to': '2dc0061c' }]],
    'e2f15a68': [['info', 'v2.2 -> v2.3: Himeko BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '6920fe29' }]],
    '27bf0a6a': [['info', 'v2.2 -> v2.3: Himeko BodyA LightMap Hash'], ['upgrade_hash', { 'to': '520336ef' }]],
    '24e4c5ad': [['info', 'v2.2 -> v2.3: Himeko BodyC Diffuse Hash'], ['upgrade_hash', { 'to': 'a769be88' }]],
    'ce965b0d': [['info', 'v2.2 -> v2.3: Himeko BodyC LightMap Hash'], ['upgrade_hash', { 'to': '094b77c6' }]],


    '3cfb3645': [['info', 'v2.2 -> v2.3: Himeko Body Position Extra Hash'], ['upgrade_hash', { 'to': '5212e2f9' }]],
    // 'd122877f': [
    //     ['info', 'v2.3: Himeko Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '5d98de11'}],
    //     ['check_hash_not_in_ini', {'hash': '3cfb3645'}],
    //     ['check_hash_not_in_ini', {'hash': '5212e2f9'}],
    //     ['multiply_section', {
    //         'titles': ['HimekoBodyPosition', 'HimekoBodyPosition_Extra'],
    //         'hashes': ['d122877f', '5212e2f9']
    //     }]
    // ],

    '5212e2f9': [['info', 'v2.3 -> v3.1: Himeko Body Position Extra Hash'], ['comment_sections', {}]],
    '947bc57e': [['info', 'v3.0 -> v3.1: Himeko Body Blend Hash'], ['upgrade_hash', { 'to': '7b8abb2a' }]],
    '71a1c8eb': [['info', 'v3.0 -> v3.1: Himeko Hair Blend Hash'], ['upgrade_hash', { 'to': '9e50b6bf' }]],
    'a8f00e3a': [['info', 'v3.0 -> v3.1: Himeko Hair Position Hash'], ['upgrade_hash', { 'to': 'b1451388' }]],
    '88093f50': [['info', 'v3.0 -> v3.1: Himeko Head Blend Hash'], ['upgrade_hash', { 'to': '67f84104' }]],
    '9ca6e275': [['info', 'v3.0 -> v3.1: Himeko Head Position Hash'], ['upgrade_hash', { 'to': '8513ffc7' }]],
    'd122877f': [['info', 'v3.0 -> v3.1: Himeko Body Position Hash'], ['upgrade_hash', { 'to': '4ba7ff4b' }]],

    '62d53b1f': [['info', 'v2.3 -> v3.2: Himeko Body Draw Hash'], ['upgrade_hash', { 'to': '6d460e12' }]],
    '7c1e9348': [['info', 'v3.1 -> v3.2: Himeko Hair Draw Hash'], ['upgrade_hash', { 'to': '738da645' }]],
    '3f65f415': [['info', 'v3.1 -> v3.2: Himeko Head Draw Hash'], ['upgrade_hash', { 'to': '30f6c118' }]],


    // MARK: Hook
    '0361b6bf': [['info', 'v1.6 -> v2.0: Hook Body Position Hash'], ['upgrade_hash', { 'to': '9d68704b' }]],
    'f1788f95': [['info', 'v1.6 -> v2.0: Hook Body Texcoord Hash'], ['upgrade_hash', { 'to': '59ccb47b' }]],
    '26276c57': [
        ['info', 'v1.6 -> v2.0: Hook Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'HookBody',
            'hash': '0c614d18',
            'trg_indices': ['0', '42189'],
            'src_indices': ['0', '-1'],
        }]
    ],

    'fcd7ee7b': [['info', 'v2.2 -> v2.3: Hook Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'f1ca01f3' }]],
    'a8e81b3a': [['info', 'v2.2 -> v2.3: Hook Hair LightMap Hash'], ['upgrade_hash', { 'to': 'db6ff34c' }]],

    'd76e33a6': [['info', 'v2.2 -> v2.3: Hook Head Diffuse Hash'], ['upgrade_hash', { 'to': '9588db54' }]],

    'b8d85743': [['info', 'v2.2 -> v2.3: Hook Body Diffuse Hash'], ['upgrade_hash', { 'to': '8ab99329' }]],
    'a49680b5': [['info', 'v2.2 -> v2.3: Hook Body LightMap Hash'], ['upgrade_hash', { 'to': '4a45ac95' }]],

    '9d68704b': [['info', 'v2.0 -> v3.1: Hook Body Position Hash'], ['upgrade_hash', { 'to': '84dd6df9' }]],
    'cf732951': [['info', 'v3.0 -> v3.1: Hook Body Blend Hash'], ['upgrade_hash', { 'to': '20825705' }]],
    '516052b3': [['info', 'v3.0 -> v3.1: Hook Hair Blend Hash'], ['upgrade_hash', { 'to': 'be912ce7' }]],
    '2c0285e5': [['info', 'v3.0 -> v3.1: Hook Hair Position Hash'], ['upgrade_hash', { 'to': '35b79857' }]],
    '0a410eec': [['info', 'v3.0 -> v3.1: Hook Head Blend Hash'], ['upgrade_hash', { 'to': 'e5b070b8' }]],
    '7d70c461': [['info', 'v3.0 -> v3.1: Hook Head Position Hash'], ['upgrade_hash', { 'to': '64c5d9d3' }]],

    'e0aa46af': [['info', 'v3.1 -> v3.2: Hook Head Draw Hash'], ['upgrade_hash', { 'to': 'ef3973a2' }]],
    'b0960adf': [['info', 'v3.1 -> v3.2: Hook Body Draw Hash'], ['upgrade_hash', { 'to': 'bf053fd2' }]],

    '53d0ba6a': [['info', 'v3.1 -> v3.2: Hook Hair Draw Hash'], ['upgrade_hash', { 'to': '5c438f67' }]],

    // MARK: Huohuo
    'd9ac0987': [['info', 'v1.6 -> v2.0: Huohuo Body Draw Hash'], ['upgrade_hash', { 'to': '67a078bd' }]],
    '6a5bb5e7': [['info', 'v1.6 -> v2.0: Huohuo Body Position Hash'], ['upgrade_hash', { 'to': 'd457c4dd' }]],
    '47dbd6aa': [['info', 'v1.6 -> v2.0: Huohuo Body Texcoord Hash'], ['upgrade_hash', { 'to': '2a306f9c' }]],
    'f05d31fb': [
        ['info', 'v1.6 -> v2.0: Huohuo Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'HuohuoBody',
            'hash': 'e9aecd0b',
            'trg_indices': ['0', '45165'],
            'src_indices': ['0', '-1'],
        }]
    ],

    'f8d072c0': [['info', 'v2.2 -> v2.3: Huohuo Hair Diffuse Hash'], ['upgrade_hash', { 'to': '057f648d' }]],
    'c0f8d106': [['info', 'v2.2 -> v2.3: Huohuo Hair LightMap Hash'], ['upgrade_hash', { 'to': '772090fc' }]],

    '7dbe20be': [['info', 'v2.2 -> v2.3: Huohuo Head Diffuse Hash'], ['upgrade_hash', { 'to': '6f1e9080' }]],

    '70d3fdb7': [['info', 'v2.2 -> v2.3: Huohuo Body Diffuse Hash'], ['upgrade_hash', { 'to': '6598aacd' }]],
    '6e5470a5': [['info', 'v2.2 -> v2.3: Huohuo Body LightMap Hash'], ['upgrade_hash', { 'to': 'afac01be' }]],

    'a1b1aafa': [['info', 'v3.0 -> v3.1: Huohuo Body Blend Hash'], ['upgrade_hash', { 'to': '4e40d4ae' }]],
    '6a7c7d6d': [['info', 'v3.0 -> v3.1: Huohuo Hair Blend Hash'], ['upgrade_hash', { 'to': '858d0339' }]],
    '21b42643': [['info', 'v3.0 -> v3.1: Huohuo Hair Position Hash'], ['upgrade_hash', { 'to': '38013bf1' }]],
    'c9bc7a6e': [['info', 'v3.0 -> v3.1: Huohuo Head Blend Hash'], ['upgrade_hash', { 'to': '264d043a' }]],
    '7c8be987': [['info', 'v3.0 -> v3.1: Huohuo Head Position Hash'], ['upgrade_hash', { 'to': '653ef435' }]],

    '67a078bd': [['info', 'v2.0 -> v3.2: Huohuo Body Draw Hash'], ['upgrade_hash', { 'to': '68334db0' }]],
    'd457c4dd': [['info', 'v2.0 -> v3.2: Huohuo Body Position Hash'], ['upgrade_hash', { 'to': '5b0744cf' }]],
    'd4259612': [['info', 'v3.1 -> v3.2: Huohuo Hair Draw Hash'], ['upgrade_hash', { 'to': 'dbb6a31f' }]],
    '96d65244': [['info', 'v3.1 -> v3.2: Huohuo Head Draw Hash'], ['upgrade_hash', { 'to': '99456749' }]],



    // MARK: Jade

    'e7048976': [['info', 'v3.0 -> v3.1: Jade Body Blend Hash'], ['upgrade_hash', { 'to': '08f5f722' }]],
    '93ba2c04': [['info', 'v3.0 -> v3.1: Jade Hair Blend Hash'], ['upgrade_hash', { 'to': '7c4b5250' }]],
    'e62b239a': [['info', 'v3.0 -> v3.1: Jade Hair Position Hash'], ['upgrade_hash', { 'to': 'ff9e3e28' }]],
    '7076a247': [['info', 'v3.0 -> v3.1: Jade Head Blend Hash'], ['upgrade_hash', { 'to': '9f87dc13' }]],
    '4c0adcc6': [['info', 'v3.0 -> v3.1: Jade Head Position Hash'], ['upgrade_hash', { 'to': '55bfc174' }]],

    '05505aaf': [['info', 'v3.1 -> v3.2: Jade Hair Draw Hash'], ['upgrade_hash', { 'to': '0ac36fa2' }]],
    'bb859078': [['info', 'v3.1 -> v3.2: Jade Head Draw Hash'], ['upgrade_hash', { 'to': 'b416a575' }]],
    'a9a4a852': [['info', 'v3.1 -> v3.2: Jade Body Draw Hash'], ['upgrade_hash', { 'to': 'a6379d5f' }]],
    '1a531432': [['info', 'v3.1 -> v3.2: Jade Body Position Hash'], ['upgrade_hash', { 'to': 'b0fed430' }]],


    // MARK: Jingliu
    '33f9fe71': [['info', 'v1.4 -> v1.6: Jingliu BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'bdbc6dce' }]],
    '67344bd9': [['info', 'v1.4 -> v1.6: Jingliu BodyA LightMap Hash'], ['upgrade_hash', { 'to': '5f55eaff' }]],
    '81c023e7': [['info', 'v1.6 -> v2.0: Jingliu Body Texcoord Hash'], ['upgrade_hash', { 'to': 'ba517fa0' }]],
    '5564183c': [
        ['info', 'v1.6 -> v2.0: Jingliu Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'JingliuBody',
            'hash': 'e8d31b6a',
            'trg_indices': ['0', '51096'],
            'src_indices': ['0', '-1'],
        }]
    ],

    '1bc1cfa0': [['info', 'v2.2 -> v2.3: Jingliu Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'f73f74cb' }]],
    'fbcefb7e': [['info', 'v2.2 -> v2.3: Jingliu Hair LightMap Hash'], ['upgrade_hash', { 'to': '70ae9680' }]],

    'c36ab82e': [['info', 'v2.2 -> v2.3: Jingliu Head Diffuse Hash'], ['upgrade_hash', { 'to': '25dd2c46' }]],

    'bdbc6dce': [['info', 'v2.2 -> v2.3: Jingliu Body Diffuse Hash'], ['upgrade_hash', { 'to': '74370924' }]],
    '5f55eaff': [['info', 'v2.2 -> v2.3: Jingliu Body LightMap Hash'], ['upgrade_hash', { 'to': 'd3a91ee8' }]],

    'cb320050': [['info', 'v3.0 -> v3.1: Jingliu Body Blend Hash'], ['upgrade_hash', { 'to': '24c37e04' }]],
    'ae0df48a': [['info', 'v3.0 -> v3.1: Jingliu Hair Blend Hash'], ['upgrade_hash', { 'to': '41fc8ade' }]],
    '35f278be': [['info', 'v3.0 -> v3.1: Jingliu Hair Position Hash'], ['upgrade_hash', { 'to': '2c47650c' }]],
    'ecf0b54f': [['info', 'v3.0 -> v3.1: Jingliu Head Blend Hash'], ['upgrade_hash', { 'to': '0301cb1b' }]],
    '6f96493b': [['info', 'v3.0 -> v3.1: Jingliu Head Position Hash'], ['upgrade_hash', { 'to': '76235489' }]],

    '11ff289a': [['info', 'v3.1 -> v3.2: Jingliu Hair Draw Hash'], ['upgrade_hash', { 'to': '1e6c1d97' }]],
    '953e1172': [['info', 'v3.1 -> v3.2: Jingliu Head Draw Hash'], ['upgrade_hash', { 'to': '9aad247f' }]],
    '73de6056': [['info', 'v3.1 -> v3.2: Jingliu Body Draw Hash'], ['upgrade_hash', { 'to': '7c4d555b' }]],
    'c029dc36': [['info', 'v3.1 -> v3.2: Jingliu Body Position Hash'], ['upgrade_hash', { 'to': '09a74ad8' }]],


    //MARK: Jiaoqiu
    'aaa5a0ff': [['info', 'v3.0 -> v3.1: Jiaoqiu Body Blend Hash'], ['upgrade_hash', { 'to': '4554deab' }]],
    '1cf0d06c': [['info', 'v3.0 -> v3.1: Jiaoqiu Hair Blend Hash'], ['upgrade_hash', { 'to': 'f301ae38' }]],
    '667ba145': [['info', 'v3.0 -> v3.1: Jiaoqiu Hair Position Hash'], ['upgrade_hash', { 'to': '7fcebcf7' }]],
    '91c5de25': [['info', 'v3.0 -> v3.1: Jiaoqiu Head Blend Hash'], ['upgrade_hash', { 'to': '7e34a071' }]],
    '4c754f6c': [['info', 'v3.0 -> v3.1: Jiaoqiu Head Position Hash'], ['upgrade_hash', { 'to': '55c052de' }]],

    '393e2b73': [['info', 'v3.1 -> v3.2: Jiaoqiu Hair Draw Hash'], ['upgrade_hash', { 'to': '36ad1e7e' }]],
    '788f41fb': [['info', 'v3.1 -> v3.2: Jiaoqiu Body Draw Hash'], ['upgrade_hash', { 'to': '771c74f6' }]],
    'cb78fd9b': [['info', 'v3.1 -> v3.2: Jiaoqiu Body Position Hash'], ['upgrade_hash', { 'to': '5d046eeb' }]],


    // MARK: JingYuan
    '8f1a29cf': [['info', 'v1.6 -> v2.0: JingYuan Body Texcoord Hash'], ['upgrade_hash', { 'to': '3423e10d' }]],
    '1be11c4f': [
        ['info', 'v1.6 -> v2.0: JingYuan Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'JingYuanBody',
            'hash': '1240ff30',
            'trg_indices': ['0', '11505', '17772', '53565'],
            'src_indices': ['0', '-1', '17772', '-1'],
        }]
    ],
    '3423e10d': [['info', 'v2.0 -> v2.1: JingYuan Body Texcoord Hash'], ['upgrade_hash', { 'to': 'ebde517e' }]],
    '1240ff30': [
        ['info', 'v2.0 -> v2.1: JingYuan Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'JingYuanBody',
            'hash': 'b2501828',
            'trg_indices': ['0', '11589', '17772', '53565'],
            'src_indices': ['0', '11505', '17772', '53565'],
        }]
    ],
    '061dd140': [['info', 'v2.0 -> v2.1: JingYuan Head Draw Hash'], ['upgrade_hash', { 'to': 'c8841602' }]],
    'ee205a7b': [['info', 'v2.0 -> v2.1: JingYuan Head Position Hash'], ['upgrade_hash', { 'to': '9d60acea' }]],
    '7c112f46': [['info', 'v2.0 -> v2.1: JingYuan Head Texcoord Hash'], ['upgrade_hash', { 'to': '20110b85' }]],
    '22147cfe': [['info', 'v2.0 -> v2.1: JingYuan Head IB Hash'], ['upgrade_hash', { 'to': 'a0459b05' }]],

    '1da0a14c': [['info', 'v2.2 -> v2.3: JingYuan Hair Diffuse Hash'], ['upgrade_hash', { 'to': '1ac1a7fb' }]],
    '97eb13d9': [['info', 'v2.2 -> v2.3: JingYuan Hair LightMap Hash'], ['upgrade_hash', { 'to': '9f47fa33' }]],

    '7dc71e05': [['info', 'v2.2 -> v2.3: JingYuan Head Diffuse Hash'], ['upgrade_hash', { 'to': 'f585da62' }]],

    '48c0277a': [['info', 'v2.2 -> v2.3: JingYuan BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '26735526' }]],
    '7dfa92fa': [['info', 'v2.2 -> v2.3: JingYuan BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'd5b2a23a' }]],
    'fd74f596': [['info', 'v2.2 -> v2.3: JingYuan BodyC Diffuse Hash'], ['upgrade_hash', { 'to': 'b1b4f581' }]],
    '9fe0c156': [['info', 'v2.2 -> v2.3: JingYuan BodyC LightMap Hash'], ['upgrade_hash', { 'to': '16a2d8bb' }]],

    'baaa1347': [['info', 'v2.4 -> v2.5: JingYuan Body Draw Hash'], ['upgrade_hash', { 'to': '0b529127' }]],
    '095daf27': [['info', 'v2.4 -> v2.5: JingYuan Body Position Hash'], ['upgrade_hash', { 'to': 'b8a52d47' }]],
    'ebde517e': [['info', 'v2.4 -> v2.5: JingYuan Body Texcoord Hash'], ['upgrade_hash', { 'to': '9f387461' }]],
    'b2501828': [
        ['info', 'v2.4 -> v2.5: JingYuan Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'JingYuanBody',
            'hash': 'b1191b83',
            'trg_indices': ['0', '11505', '17778', '53571'],
            'src_indices': ['0', '11589', '17772', '53565'],
        }]
    ],

    '9d60acea': [['info', 'v2.1 -> v3.1: JingYuan Head Position Hash'], ['upgrade_hash', { 'to': '84d5b158' }]],
    'cc58b451': [['info', 'v3.0 -> v3.1: JingYuan Body Blend Hash'], ['upgrade_hash', { 'to': '23a9ca05' }]],
    'e1d746fc': [['info', 'v3.0 -> v3.1: JingYuan Hair Blend Hash'], ['upgrade_hash', { 'to': '0e2638a8' }]],
    '31319f5f': [['info', 'v3.0 -> v3.1: JingYuan Hair Position Hash'], ['upgrade_hash', { 'to': '288482ed' }]],
    '61d571f5': [['info', 'v3.0 -> v3.1: JingYuan Head Blend Hash'], ['upgrade_hash', { 'to': '8e240fa1' }]],

    'c8841602': [['info', 'v2.1 -> v3.2: JingYuan Head Draw Hash'], ['upgrade_hash', { 'to': 'c717230f' }]],
    '0b529127': [['info', 'v2.5 -> v3.2: JingYuan Body Draw Hash'], ['upgrade_hash', { 'to': '04c1a42a' }]],
    'b8a52d47': [['info', 'v2.5 -> v3.2: JingYuan Body Position Hash'], ['upgrade_hash', { 'to': '5209eaca' }]],
    '457fc548': [['info', 'v3.1 -> v3.2: JingYuan Hair Draw Hash'], ['upgrade_hash', { 'to': '4aecf045' }]],


    // MARK: Kafka
    '51abd7c9': [['info', 'v1.4 -> v1.6: Kafka Body Position Hash'], ['upgrade_hash', { 'to': 'deb266a8' }]],
    '38072744': [['info', 'v1.4 -> v1.6: Kafka Body Position Extra Hash'], ['upgrade_hash', { 'to': '17cb3b3e' }]],
    'a6813fd5': [['info', 'v1.4 -> v1.6: Kafka Body Texcoord Hash'], ['upgrade_hash', { 'to': '190e483a' }]],
    'b7401039': [['info', 'v1.4 -> v1.6: Kafka Body IB Hash'], ['upgrade_hash', { 'to': '8d847042' }]],

    '17cb3b3e': [['info', 'v1.6 -> v2.0: Kafka Body Position Extra Hash'], ['upgrade_hash', { 'to': 'cd2222f8' }]],
    '190e483a': [['info', 'v1.6 -> v2.0: Kafka Body Texcoord Hash'], ['upgrade_hash', { 'to': '05ded7f7' }]],
    'e25c6ba9': [['info', 'v1.6 -> v2.0: Kafka Body Draw Hash'], ['upgrade_hash', { 'to': '6d45dac8' }]],
    '8d847042': [
        ['info', 'v1.6 -> v2.0: Kafka Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'KafkaBody',
            'hash': 'fa23099d',
            'trg_indices': ['0', '8787', '16083', '35439', '41406'],
            'src_indices': ['0', '-1', '16083', '-1', '41406'],
        }]
    ],
    // 'deb266a8': [
    //     ['info', 'v2.1: Kafka Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': 'cd2222f8'}],
    //     ['check_hash_not_in_ini', {'hash': '17cb3b3e'}],
    //     ['check_hash_not_in_ini', {'hash': '38072744'}],
    //     ['multiply_section', {
    //         'titles': ['KafkaBodyPosition', 'KafkaBodyPosition_Extra'],
    //         'hashes': ['deb266a8', 'cd2222f8']
    //     }]
    // ],

    'cd60c900': [['info', 'v2.2 -> v2.3: Kafka Hair Texcoord Hash'], ['upgrade_hash', { 'to': 'ddbe6ba2' }]],
    '55d258a5': [['info', 'v2.2 -> v2.3: Kafka Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'cb354b6b' }]],
    'dc6aaf17': [['info', 'v2.2 -> v2.3: Kafka Hair LightMap Hash'], ['upgrade_hash', { 'to': 'e07efe45' }]],

    '1d74e2f5': [['info', 'v2.2 -> v2.3: Kafka Head Diffuse Hash'], ['upgrade_hash', { 'to': 'cf90e442' }]],

    '05ded7f7': [['info', 'v2.2 -> v2.3: Kafka Body Texcoord Hash'], ['upgrade_hash', { 'to': 'd14b435e' }]],
    '0da4c671': [['info', 'v2.2 -> v2.3: Kafka BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '207c0559' }]],
    'cc322c0f': [['info', 'v2.2 -> v2.3: Kafka BodyA LightMap Hash'], ['upgrade_hash', { 'to': '32b5b281' }]],
    '339785c4': [['info', 'v2.2 -> v2.3: Kafka BodyA StockingMap Hash'], ['upgrade_hash', { 'to': 'fd0ef162' }]],

    'e8e2b6da': [['info', 'v2.2 -> v2.3: Kafka BodyC Diffuse Hash'], ['upgrade_hash', { 'to': 'c00b55bc' }]],
    '7bd0d180': [['info', 'v2.2 -> v2.3: Kafka BodyC LightMap Hash'], ['upgrade_hash', { 'to': '45d15ffb' }]],

    'cd2222f8': [['info', 'v2.0 -> v3.1: Kafka Body Position Extra Hash'], ['comment_sections', {}]],
    '4babbbd9': [['info', 'v3.0 -> v3.1: Kafka Body Blend Hash'], ['upgrade_hash', { 'to': 'a45ac58d' }]],
    '91133916': [['info', 'v3.0 -> v3.1: Kafka Hair Blend Hash'], ['upgrade_hash', { 'to': '7ee24742' }]],
    'cdda77a3': [['info', 'v3.0 -> v3.1: Kafka Hair Position Hash'], ['upgrade_hash', { 'to': 'd46f6a11' }]],
    'e811b655': [['info', 'v3.0 -> v3.1: Kafka Head Blend Hash'], ['upgrade_hash', { 'to': '07e0c801' }]],
    '7cbe836d': [['info', 'v3.0 -> v3.1: Kafka Head Position Hash'], ['upgrade_hash', { 'to': '650b9edf' }]],
    'deb266a8': [['info', 'v3.0 -> v3.1: Kafka Body Position Hash'], ['upgrade_hash', { 'to': 'd4973f4a' }]],

    '6d45dac8': [['info', 'v2.0 -> v3.2: Kafka Body Draw Hash'], ['upgrade_hash', { 'to': '62d6efc5' }]],
    '132595c5': [['info', 'v3.1 -> v3.2: Kafka Hair Draw Hash'], ['upgrade_hash', { 'to': '1cb6a0c8' }]],
    '48576da3': [['info', 'v3.1 -> v3.2: Kafka Head Draw Hash'], ['upgrade_hash', { 'to': '47c458ae' }]],


    // MARK: Lingsha
    'ea4c4532': [['info', 'v3.0 -> v3.1: Lingsha Body Blend Hash'], ['upgrade_hash', { 'to': '05bd3b66' }]],
    'bc787aec': [['info', 'v3.0 -> v3.1: Lingsha Hair Blend Hash'], ['upgrade_hash', { 'to': '538904b8' }]],
    'c207a096': [['info', 'v3.0 -> v3.1: Lingsha Hair Position Hash'], ['upgrade_hash', { 'to': 'dbb2bd24' }]],
    'dc9cba18': [['info', 'v3.0 -> v3.1: Lingsha Head Blend Hash'], ['upgrade_hash', { 'to': '336dc44c' }]],
    'e779f220': [['info', 'v3.0 -> v3.1: Lingsha Head Position Hash'], ['upgrade_hash', { 'to': 'feccef92' }]],

    'bbe8b08e': [['info', 'v3.1 -> v3.2: Lingsha Hair Draw Hash'], ['upgrade_hash', { 'to': 'b47b8583' }]],
    '3b530692': [['info', 'v3.1 -> v3.2: Lingsha Head Draw Hash'], ['upgrade_hash', { 'to': '34c0339f' }]],
    '940b1b19': [['info', 'v3.1 -> v3.2: Lingsha Body Draw Hash'], ['upgrade_hash', { 'to': '9b982e14' }]],
    '27fca779': [['info', 'v3.1 -> v3.2: Lingsha Body Position Hash'], ['upgrade_hash', { 'to': 'b1dd664a' }]],


    // MARK: Luka
    'e0c63ed8': [['info', 'v1.4 -> v1.6: Luka BodyA LightMap Hash'], ['upgrade_hash', { 'to': '31724118' }]],
    '78d83281': [['info', 'v1.4 -> v1.6: Luka BodyB LightMap Hash'], ['upgrade_hash', { 'to': '58749091' }]],

    'f7d86ef0': [['info', 'v1.6 -> v2.0: Luka Body Position Extra Hash'], ['upgrade_hash', { 'to': '3e55d897' }]],
    '098a46fc': [['info', 'v1.6 -> v2.0: Luka Body Texcoord Hash'], ['upgrade_hash', { 'to': '11dd3da1' }]],
    '5cd5d088': [['info', 'v1.6 -> v2.0: Luka BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '3ba22ed5' }]],
    '148d7790': [['info', 'v1.6 -> v2.0: Luka BodyB Diffuse Hash'], ['upgrade_hash', { 'to': '73fa89cd' }]],
    '5332e0c4': [
        ['info', 'v1.6 -> v2.0: Luka Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'LukaBody',
            'hash': 'e0c9f7ec',
            'trg_indices': ['0', '25371', '49992', '52830'],
            'src_indices': ['0', '28209', '-1', '-1'],
        }]
    ],
    // '03fba4b4': [
    //     ['info', 'v2.1: Luka Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '3e55d897'}],
    //     ['check_hash_not_in_ini', {'hash': 'f7d86ef0'}],
    //     ['multiply_section', {
    //         'titles': ['LukaBodyPosition', 'LukaBodyPosition_Extra'],
    //         'hashes': ['03fba4b4', '3e55d897']
    //     }]
    // ],

    '2427134f': [['info', 'v2.2 -> v2.3: Luka Hair Diffuse Hash'], ['upgrade_hash', { 'to': '6e34ac83' }]],
    'c6b43fae': [['info', 'v2.2 -> v2.3: Luka Hair LightMap Hash'], ['upgrade_hash', { 'to': '6d784dff' }]],

    '4d8ef1d8': [['info', 'v2.2 -> v2.3: Luka Head Diffuse Hash'], ['upgrade_hash', { 'to': 'e8d263c3' }]],

    '3ba22ed5': [['info', 'v2.2 -> v2.3: Luka BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'a026c901' }]],
    '31724118': [['info', 'v2.2 -> v2.3: Luka BodyA LightMap Hash'], ['upgrade_hash', { 'to': '1762e62c' }]],
    '73fa89cd': [['info', 'v2.2 -> v2.3: Luka BodyB Diffuse Hash'], ['upgrade_hash', { 'to': '00970f33' }]],
    '58749091': [['info', 'v2.2 -> v2.3: Luka BodyB LightMap Hash'], ['upgrade_hash', { 'to': '31483729' }]],

    '3e55d897': [['info', 'v2.0 -> v3.1: Luka Body Position Extra Hash'], ['comment_sections', {}]],
    'e98c3d24': [['info', 'v3.0 -> v3.1: Luka Hair Blend Hash'], ['upgrade_hash', { 'to': '067d4370' }]],
    'a96bca86': [['info', 'v3.0 -> v3.1: Luka Hair Position Hash'], ['upgrade_hash', { 'to': 'b0ded734' }]],
    '614df023': [['info', 'v3.0 -> v3.1: Luka Head Blend Hash'], ['upgrade_hash', { 'to': '8ebc8e77' }]],
    '222b9650': [['info', 'v3.0 -> v3.1: Luka Head Position Hash'], ['upgrade_hash', { 'to': '3b9e8be2' }]],
    '03fba4b4': [['info', 'v3.0 -> v3.1: Luka Body Position Hash'], ['upgrade_hash', { 'to': '27e0c525' }]],

    '934c42a2': [['info', 'v3.1 -> v3.2: Luka Hair Draw Hash'], ['upgrade_hash', { 'to': '9cdf77af' }]],
    'a8cf872d': [['info', 'v3.1 -> v3.2: Luka Head Draw Hash'], ['upgrade_hash', { 'to': 'a75cb220' }]],
    'b00c18d4': [['info', 'v3.1 -> v3.2: Luka Body Draw Hash'], ['upgrade_hash', { 'to': 'bf9f2dd9' }]],
    '8ddf531a': [['info', 'v3.1 -> v3.2: Luka Body Blend Hash'], ['upgrade_hash', { 'to': 'd012ae01' }]],


    // MARK: Luocha
    'b5c61afb': [['info', 'v1.6 -> v2.0: Luocha Body Draw Hash'], ['upgrade_hash', { 'to': '194a6495' }]],
    '0631a69b': [['info', 'v1.6 -> v2.0: Luocha Body Position Hash'], ['upgrade_hash', { 'to': 'aabdd8f5' }]],
    'a67c4fed': [['info', 'v1.6 -> v2.0: Luocha Body Texcoord Hash'], ['upgrade_hash', { 'to': '80da6fb8' }]],
    '6c659c20': [
        ['info', 'v1.6 -> v2.0: Luocha Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'LuochaBody',
            'hash': '149a218b',
            'trg_indices': ['0', '4503', '34437', '45126'],
            'src_indices': ['0', '-1', '34437', '-1'],
        }]
    ],

    '17542aca': [['info', 'v2.2 -> v2.3: Luocha Hair Diffuse Hash'], ['upgrade_hash', { 'to': '9420ae03' }]],
    'dadf8929': [['info', 'v2.2 -> v2.3: Luocha Hair LightMap Hash'], ['upgrade_hash', { 'to': 'a7e6fa4f' }]],

    '8af54c5d': [['info', 'v2.2 -> v2.3: Luocha Head Diffuse Hash'], ['upgrade_hash', { 'to': '664f2f29' }]],

    'f9d9adb8': [['info', 'v2.2 -> v2.3: Luocha BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '7185fd68' }]],
    'd8dd2b05': [['info', 'v2.2 -> v2.3: Luocha BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'eb99eb88' }]],
    'a1fac228': [['info', 'v2.2 -> v2.3: Luocha BodyC Diffuse Hash'], ['upgrade_hash', { 'to': '65dec275' }]],
    'ff928485': [['info', 'v2.2 -> v2.3: Luocha BodyC LightMap Hash'], ['upgrade_hash', { 'to': '45feb69d' }]],

    '0a1b224a': [['info', 'v3.0 -> v3.1: Luocha Body Blend Hash'], ['upgrade_hash', { 'to': 'e5ea5c1e' }]],
    '5484d0a4': [['info', 'v3.0 -> v3.1: Luocha Hair Blend Hash'], ['upgrade_hash', { 'to': 'bb75aef0' }]],
    '2f7b8290': [['info', 'v3.0 -> v3.1: Luocha Hair Position Hash'], ['upgrade_hash', { 'to': '36ce9f22' }]],
    '02962a1d': [['info', 'v3.0 -> v3.1: Luocha Head Blend Hash'], ['upgrade_hash', { 'to': 'ed675449' }]],
    'd5f4ef26': [['info', 'v3.0 -> v3.1: Luocha Head Position Hash'], ['upgrade_hash', { 'to': 'cc41f294' }]],

    '194a6495': [['info', 'v2.0 -> v3.2: Luocha Body Draw Hash'], ['upgrade_hash', { 'to': '16d95198' }]],
    'aabdd8f5': [['info', 'v2.0 -> v3.2: Luocha Body Position Hash'], ['upgrade_hash', { 'to': 'd16e462c' }]],
    'ad6bae51': [['info', 'v3.1 -> v3.2: Luocha Hair Draw Hash'], ['upgrade_hash', { 'to': 'a2f89b5c' }]],
    '7c78988c': [['info', 'v3.1 -> v3.2: Luocha Head Draw Hash'], ['upgrade_hash', { 'to': '73ebad81' }]],


    // MARK: Lynx
    '8e595209': [['info', 'v1.6 -> v2.0: Lynx Body Texcoord Hash'], ['upgrade_hash', { 'to': '52a44eba' }]],
    'b6019d61': [['info', 'v1.6 -> v2.0: Lynx BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'e2bad880' }]],
    'e8c4b27f': [
        ['info', 'v1.6 -> v2.0: Lynx Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'LynxBody',
            'hash': '71647b48',
            'trg_indices': ['0', '51510'],
            'src_indices': ['0', '-1'],
        }]
    ],

    '6d27e7f2': [['info', 'v2.2 -> v2.3: Lynx Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'f4db275c' }]],
    'a874888b': [['info', 'v2.2 -> v2.3: Lynx Hair LightMap Hash'], ['upgrade_hash', { 'to': '8dc79479' }]],

    '3e2ad9b8': [['info', 'v2.2 -> v2.3: Lynx Head Diffuse Hash'], ['upgrade_hash', { 'to': 'e5d8fa29' }]],

    '52a44eba': [['info', 'v2.2 -> v2.3: Lynx Body Texcoord Hash'], ['upgrade_hash', { 'to': 'bffadc55' }]],
    'e2bad880': [['info', 'v2.2 -> v2.3: Lynx Body Diffuse Hash'], ['upgrade_hash', { 'to': '6c664cc4' }]],
    '6cb92f15': [['info', 'v2.2 -> v2.3: Lynx Body LightMap Hash'], ['upgrade_hash', { 'to': '540bf4e4' }]],

    // '09667bf6': [
    //     ['info', 'v2.3: Lynx Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '09667bf6'}],
    //     ['check_hash_not_in_ini', {'hash': '7b23e3e6'}],
    //     ['multiply_section', {
    //         'titles': ['LynxBodyPosition', 'LynxBodyPosition_Extra'],
    //         'hashes': ['09667bf6', '7b23e3e6']
    //     }]
    // ],

    '7b23e3e6': [['info', 'v2.3 -> v3.1: Lynx Body Position Hash Extra'], ['comment_sections', {}]],
    'fa9c73b8': [['info', 'v3.0 -> v3.1: Lynx Body Blend Hash'], ['upgrade_hash', { 'to': '156d0dec' }]],
    '195d3f53': [['info', 'v3.0 -> v3.1: Lynx Hair Blend Hash'], ['upgrade_hash', { 'to': 'f6ac4107' }]],
    '7ab99fa3': [['info', 'v3.0 -> v3.1: Lynx Hair Position Hash'], ['upgrade_hash', { 'to': '630c8211' }]],
    'de5813fa': [['info', 'v3.0 -> v3.1: Lynx Head Blend Hash'], ['upgrade_hash', { 'to': '31a96dae' }]],
    'b636d476': [['info', 'v3.0 -> v3.1: Lynx Head Position Hash'], ['upgrade_hash', { 'to': 'af83c9c4' }]],
    '09667bf6': [['info', 'v3.0 -> v3.1: Lynx Body Position Hash'], ['upgrade_hash', { 'to': '6296fe54' }]],

    'e46f88d6': [['info', 'v3.1 -> v3.2: Lynx Hair Draw Hash'], ['upgrade_hash', { 'to': 'ebfcbddb' }]],
    '407d2f48': [['info', 'v3.1 -> v3.2: Lynx Head Draw Hash'], ['upgrade_hash', { 'to': '4fee1a45' }]],
    'ba91c796': [['info', 'v3.1 -> v3.2: Lynx Body Draw Hash'], ['upgrade_hash', { 'to': 'b502f29b' }]],


    // MARK: March7th
    'fcef8885': [['info', 'v1.6 -> v2.0: March7th Body Texcoord Hash'], ['upgrade_hash', { 'to': 'ecf4648c' }]],
    '97ad7623': [
        ['info', 'v1.6 -> v2.0: March7th Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'March7thBody',
            'hash': '5212ce68',
            'trg_indices': ['0', '30828', '41466', '53751'],
            'src_indices': ['0', '32502', '-1', '-1'],
        }]
    ],

    '1ed7e59d': [['info', 'v2.2 -> v2.3: March7th Hair Texcoord Hash'], ['upgrade_hash', { 'to': '948c4e59' }]],
    '6bd71ad9': [['info', 'v2.2 -> v2.3: March7th Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'e299099f' }]],
    '371ca498': [['info', 'v2.2 -> v2.3: March7th Hair LightMap Hash'], ['upgrade_hash', { 'to': '89cd27c7' }]],

    '2d25d041': [['info', 'v2.2 -> v2.3: March7th Head Diffuse Hash'], ['upgrade_hash', { 'to': 'dbbb9b12' }]],

    'ecf4648c': [['info', 'v2.2 -> v2.3: March7th Body Texcoord Hash'], ['upgrade_hash', { 'to': 'b950fe40' }]],
    'e6b35ac0': [['info', 'v2.2 -> v2.3: March7th BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'a9101746' }]],
    '8c584d30': [['info', 'v2.2 -> v2.3: March7th BodyA LightMap Hash'], ['upgrade_hash', { 'to': '87f4596d' }]],
    'b57574b3': [['info', 'v2.2 -> v2.3: March7th BodyB Diffuse Hash'], ['upgrade_hash', { 'to': 'cada1307' }]],
    '2006cd6a': [['info', 'v2.2 -> v2.3: March7th BodyB LightMap Hash'], ['upgrade_hash', { 'to': '01f9dbb8' }]],

    'baaeaaf0': [['info', 'v3.0 -> v3.1: March7th Body Blend Hash'], ['upgrade_hash', { 'to': '555fd4a4' }]],
    '749c6d11': [['info', 'v3.0 -> v3.1: March7th Hair Blend Hash'], ['upgrade_hash', { 'to': '9b6d1345' }]],
    '09903e8e': [['info', 'v3.0 -> v3.1: March7th Hair Position Hash'], ['upgrade_hash', { 'to': '1025233c' }]],
    '694f577b': [['info', 'v3.0 -> v3.1: March7th Head Blend Hash'], ['upgrade_hash', { 'to': '86be292f' }]],
    'b23869e0': [['info', 'v3.0 -> v3.1: March7th Head Position Hash'], ['upgrade_hash', { 'to': 'ab8d7452' }]],

    'be2f8ed9': [['info', 'v3.1 -> v3.2: March7thPreservation Hair Draw Hash'], ['upgrade_hash', { 'to': 'b1bcbbd4' }]],
    'e6654dbb': [['info', 'v3.1 -> v3.2: March7thPreservation Body Draw Hash'], ['upgrade_hash', { 'to': 'e9f678b6' }]],
    '5592f1db': [['info', 'v3.1 -> v3.2: March7thPreservation Body Position Hash'], ['upgrade_hash', { 'to': 'f5491fea' }]],


    // MARK: March7thHunt

    '712bac55': [['info', 'v3.0 -> v3.1: March7thHunt Body Blend Hash'], ['upgrade_hash', { 'to': '9edad201' }]],
    '6f468e3f': [['info', 'v3.0 -> v3.1: March7thHunt Hair Blend Hash'], ['upgrade_hash', { 'to': '80b7f06b' }]],
    '411425d0': [['info', 'v3.0 -> v3.1: March7thHunt Hair Position Hash'], ['upgrade_hash', { 'to': '58a13862' }]],

    '99361f97': [['info', 'v3.1 -> v3.2: March7thHunt Hair Draw Hash'], ['upgrade_hash', { 'to': '96a52a9a' }]], // same hash as ruan mei
    '49a6a173': [['info', 'v3.1 -> v3.2: March7thHunt Head Draw Hash'], ['upgrade_hash', { 'to': '4635947e' }]],
    '5764cafb': [['info', 'v3.1 -> v3.2: March7thHunt Body Draw Hash'], ['upgrade_hash', { 'to': '58f7fff6' }]],
    'e493769b': [['info', 'v3.1 -> v3.2: March7thHunt Body Position Hash'], ['upgrade_hash', { 'to': 'e0454a9f' }]],



    //MARK: MEM
    // '65e20e67': [['info', 'v3.1 -> v3.2: Position Hash --> Blend Hash'], ['upgrade_hash', {'to': '1066f7e6'}]],
    "dfecbcfd": [['info', 'v3.1 -> v3.2: Mem Draw Hash'], ['upgrade_hash', { 'to': 'd07f89f0' }]],

    // MARK: Misha
    '0f570849': [['info', 'v2.0 -> v2.1: Misha Head Position Hash'], ['upgrade_hash', { 'to': 'be8ee647' }]],
    '8aa3d867': [['info', 'v2.0 -> v2.1: Misha Head Texcoord Hash'], ['upgrade_hash', { 'to': 'ee650b42' }]],

    'c49badcb': [['info', 'v2.2 -> v2.3: Misha Hair Draw Hash'], ['upgrade_hash', { 'to': 'cdc4b6ac' }]],
    '4b221f10': [['info', 'v2.2 -> v2.3: Misha Hair Position Hash'], ['upgrade_hash', { 'to': 'af206cba' }]],
    '9980f41b': [['info', 'v2.2 -> v2.3: Misha Hair Texcoord Hash'], ['upgrade_hash', { 'to': 'e35c9a5a' }]],
    '66e3518a': [['info', 'v2.2 -> v2.3: Misha Hair IB Hash'], ['upgrade_hash', { 'to': '08e4fb11' }]],
    '028905ee': [['info', 'v2.2 -> v2.3: Misha Hair Diffuse Hash'], ['upgrade_hash', { 'to': '328e0604' }]],
    '8e793185': [['info', 'v2.2 -> v2.3: Misha Hair LightMap Hash'], ['upgrade_hash', { 'to': 'f66cebd0' }]],

    'ee650b42': [['info', 'v2.2 -> v2.3: Misha Head Texcoord Hash'], ['upgrade_hash', { 'to': '7abbb9e1' }]],
    '958056b6': [['info', 'v2.2 -> v2.3: Misha Head Diffuse Hash'], ['upgrade_hash', { 'to': '60707bff' }]],

    '157dc503': [['info', 'v2.2 -> v2.3: Misha Body Diffuse Hash'], ['upgrade_hash', { 'to': '2b17a6a5' }]],
    '429f63a8': [['info', 'v2.2 -> v2.3: Misha Body LightMap Hash'], ['upgrade_hash', { 'to': 'ce79ee01' }]],

    'be8ee647': [['info', 'v2.1 -> v3.1: Misha Head Position Hash'], ['upgrade_hash', { 'to': 'a73bfbf5' }]],
    'af206cba': [['info', 'v2.3 -> v3.1: Misha Hair Position Hash'], ['upgrade_hash', { 'to': 'b6957108' }]],
    '7cbca09c': [['info', 'v3.0 -> v3.1: Misha Body Blend Hash'], ['upgrade_hash', { 'to': '934ddec8' }]],
    '3ae2fc69': [['info', 'v3.0 -> v3.1: Misha Hair Blend Hash'], ['upgrade_hash', { 'to': 'd513823d' }]],
    '999dff73': [['info', 'v3.0 -> v3.1: Misha Head Blend Hash'], ['upgrade_hash', { 'to': '766c8127' }]],

    'cdc4b6ac': [['info', 'v2.3 -> v3.2: Misha Hair Draw Hash'], ['upgrade_hash', { 'to': 'c25783a1' }]],
    '56ce3fd0': [['info', 'v3.1 -> v3.2: Misha Head Draw Hash'], ['upgrade_hash', { 'to': '595d0add' }]],
    '6652bfc8': [['info', 'v3.1 -> v3.2: Misha Body Draw Hash'], ['upgrade_hash', { 'to': '69c18ac5' }]],
    'd5a503a8': [['info', 'v3.1 -> v3.2: Misha Body Position Hash'], ['upgrade_hash', { 'to': 'd5d9dd08' }]],


    // MARK: Moze
    'dda2bf74': [['info', 'v2.6 -> v2.7: Moze Head Texcoord Hash'], ['upgrade_hash', { 'to': '1a604ceb' }]],
    '7439f4c8': [['info', 'v2.6 -> v2.7: Moze Body Texcoord Hash'], ['upgrade_hash', { 'to': '84a33c6c' }]],

    '86630c6b': [['info', 'v3.0 -> v3.1: Moze Body Blend Hash'], ['upgrade_hash', { 'to': '6992723f' }]],
    '49534d8c': [['info', 'v3.0 -> v3.1: Moze Hair Blend Hash'], ['upgrade_hash', { 'to': 'a6a233d8' }]],
    'c88fe0e7': [['info', 'v3.0 -> v3.1: Moze Hair Position Hash'], ['upgrade_hash', { 'to': 'd13afd55' }]],
    '07f88cc9': [['info', 'v3.0 -> v3.1: Moze Head Blend Hash'], ['upgrade_hash', { 'to': 'e809f29d' }]],
    '48ddd366': [['info', 'v3.0 -> v3.1: Moze Head Position Hash'], ['upgrade_hash', { 'to': '5168ced4' }]],

    '806542dc': [['info', 'v3.1 -> v3.2: Moze Hair Draw Hash'], ['upgrade_hash', { 'to': '8ff677d1' }]],
    '0bcc8d2f': [['info', 'v3.1 -> v3.2: Moze Body Draw Hash'], ['upgrade_hash', { 'to': '045fb822' }]],
    'b83b314f': [['info', 'v3.1 -> v3.2: Moze Body Position Hash'], ['upgrade_hash', { 'to': '590ae82e' }]],

    // Mark: Mydei
    '2b0abf3c': [['info', 'v3.1 -> v3.2: Mydei Hair Draw Hash'], ['upgrade_hash', { 'to': '24998a31' }]],
    '5ee19070': [['info', 'v3.1 -> v3.2: Mydei Head Draw Hash'], ['upgrade_hash', { 'to': '5172a57d' }]],
    'dd69365a': [['info', 'v3.1 -> v3.2: Mydei Body Draw Hash'], ['upgrade_hash', { 'to': 'd2fa0357' }]],



    // MARK: Natasha
    'fc66ad46': [['info', 'v1.6 -> v2.0: Natasha Body Position Extra Hash'], ['upgrade_hash', { 'to': '4958a3f3' }]],
    '9ac894b4': [['info', 'v1.6 -> v2.0: Natasha Body Texcoord Hash'], ['upgrade_hash', { 'to': 'b9b8b2a1' }]],
    '005670d8': [
        ['info', 'v1.6 -> v2.0: Natasha Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'NatashaBody',
            'hash': '68dd15e8',
            'trg_indices': ['0', '3024', '38907', '45612'],
            'src_indices': ['-1', '0', '-1', '38907'],
        }]
    ],
    // '0de1ff21': [
    //     ['info', 'v2.1: Natasha Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '4958a3f3'}],
    //     ['check_hash_not_in_ini', {'hash': 'fc66ad46'}],
    //     ['multiply_section', {
    //         'titles': ['NatashaBodyPosition', 'NatashaBodyPosition_Extra'],
    //         'hashes': ['0de1ff21', '4958a3f3']
    //     }]
    // ],

    '5f44fc0d': [['info', 'v2.2 -> v2.3: Natasha Hair Texcoord Hash'], ['upgrade_hash', { 'to': 'a9728390' }]],
    '595464a6': [['info', 'v2.2 -> v2.3: Natasha Hair Diffuse Hash'], ['upgrade_hash', { 'to': '08ac31d1' }]],
    'abcc21d1': [['info', 'v2.2 -> v2.3: Natasha Hair LightMap Hash'], ['upgrade_hash', { 'to': '260f2286' }]],

    '5a9597db': [['info', 'v2.2 -> v2.3: Natasha Head Diffuse Hash'], ['upgrade_hash', { 'to': 'b719225a' }]],

    'b9b8b2a1': [['info', 'v2.2 -> v2.3: Natasha Body Texcoord Hash'], ['upgrade_hash', { 'to': 'f1668e08' }]],
    '209f5c65': [['info', 'v2.2 -> v2.3: Natasha BodyB Diffuse Hash'], ['upgrade_hash', { 'to': '6f4ab910' }]],
    'bfd47fe8': [['info', 'v2.2 -> v2.3: Natasha BodyB LightMap Hash'], ['upgrade_hash', { 'to': 'fe813491' }]],
    '88be8df6': [['info', 'v2.2 -> v2.3: Natasha BodyB StockingMap Hash'], ['upgrade_hash', { 'to': 'defb30fc' }]],
    '3bd51af4': [['info', 'v2.2 -> v2.3: Natasha BodyD Diffuse Hash'], ['upgrade_hash', { 'to': '519ef69f' }]],
    '2799f499': [['info', 'v2.2 -> v2.3: Natasha BodyD LightMap Hash'], ['upgrade_hash', { 'to': '919da513' }]],
    'de96634b': [['info', 'v2.2 -> v2.3: Natasha BodyD StockingMap Hash'], ['upgrade_hash', { 'to': '236df0fa' }]],

    '4958a3f3': [['info', 'v2.0 -> v3.1: Natasha Body Position Extra Hash'], ['comment_sections', {}]],
    '23c6f5ac': [['info', 'v3.0 -> v3.1: Natasha Body Blend Hash'], ['upgrade_hash', { 'to': 'cc378bf8' }]],
    '38e692c7': [['info', 'v3.0 -> v3.1: Natasha Hair Blend Hash'], ['upgrade_hash', { 'to': 'd717ec93' }]],
    '29539b93': [['info', 'v3.0 -> v3.1: Natasha Hair Position Hash'], ['upgrade_hash', { 'to': '30e68621' }]],
    '94322ac5': [['info', 'v3.0 -> v3.1: Natasha Head Blend Hash'], ['upgrade_hash', { 'to': '7bc35491' }]],
    '4a197424': [['info', 'v3.0 -> v3.1: Natasha Head Position Hash'], ['upgrade_hash', { 'to': '53ac6996' }]],
    '0de1ff21': [['info', 'v3.0 -> v3.1: Natasha Body Position Hash'], ['upgrade_hash', { 'to': '50edbe41' }]],

    '4eb6efa7': [['info', 'v3.1 -> v3.2: Natasha Hair Draw Hash'], ['upgrade_hash', { 'to': '4125daaa' }]],
    'be164341': [['info', 'v3.1 -> v3.2: Natasha Body Draw Hash'], ['upgrade_hash', { 'to': 'b185764c' }]],


    // MARK: Pela
    '6148b897': [['info', 'v1.6 -> v2.0: Pela Body Texcoord Hash'], ['upgrade_hash', { 'to': '77a2f3bf' }]],
    'f4eb23b2': [
        ['info', 'v1.6 -> v2.0: Pela Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'PelaBody',
            'hash': '98dbd548',
            'trg_indices': ['0', '44043'],
            'src_indices': ['0', '-1'],
        }]
    ],

    '934172e5': [['info', 'v2.2 -> v2.3: Pela Hair Diffuse Hash'], ['upgrade_hash', { 'to': '7fcd70ea' }]],
    '54a11a98': [['info', 'v2.2 -> v2.3: Pela Hair LightMap Hash'], ['upgrade_hash', { 'to': '93279a4a' }]],

    '0a50c14c': [['info', 'v2.2 -> v2.3: Pela Head Diffuse Hash'], ['upgrade_hash', { 'to': '945d61df' }]],

    'e02d100c': [['info', 'v2.2 -> v2.3: Pela Body Diffuse Hash'], ['upgrade_hash', { 'to': '48fca7f8' }]],
    'ffeb1d46': [['info', 'v2.2 -> v2.3: Pela Body LightMap Hash'], ['upgrade_hash', { 'to': '21d34147' }]],
    '8df14d0a': [['info', 'v2.2 -> v2.3: Pela Body StockingMap Hash'], ['upgrade_hash', { 'to': '883e4c54' }]],

    'b889667f': [['info', 'v3.0 -> v3.1: Pela Body Blend Hash'], ['upgrade_hash', { 'to': '5778182b' }]],
    '4b1780bb': [['info', 'v3.0 -> v3.1: Pela Hair Blend Hash'], ['upgrade_hash', { 'to': 'a4e6feef' }]],
    'fd24333c': [['info', 'v3.0 -> v3.1: Pela Hair Position Hash'], ['upgrade_hash', { 'to': 'e4912e8e' }]],
    '53070a34': [['info', 'v3.0 -> v3.1: Pela Head Blend Hash'], ['upgrade_hash', { 'to': 'bcf67460' }]],
    'db053da4': [['info', 'v3.0 -> v3.1: Pela Head Position Hash'], ['upgrade_hash', { 'to': 'c2b02016' }]],

    '18d9ad82': [['info', 'v3.1 -> v3.2: Pela Hair Draw Hash'], ['upgrade_hash', { 'to': '174a988f' }]],
    '87f9b9c3': [['info', 'v3.1 -> v3.2: Pela Head Draw Hash'], ['upgrade_hash', { 'to': '886a8cce' }]],
    '79a43d63': [['info', 'v3.1 -> v3.2: Pela Body Draw Hash'], ['upgrade_hash', { 'to': '7637086e' }]],
    'ca538103': [['info', 'v3.1 -> v3.2: Pela Body Position Hash'], ['upgrade_hash', { 'to': '27b7176f' }]],


    // MARK: Qingque
    '3a305670': [['info', 'v1.6 -> v2.0: Qingque Body Texcoord Hash'], ['upgrade_hash', { 'to': 'cc2db614' }]],
    'daafea36': [
        ['info', 'v1.6 -> v2.0: Qingque Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'QingqueBody',
            'hash': '0a82ceb7',
            'trg_indices': ['0', '23730', '27573', '45615'],
            'src_indices': ['0', '-1', '-1', '27765'],
        }]
    ],

    '73fbbace': [['info', 'v2.2 -> v2.3: Qingque Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'd9e91d27' }]],
    '48829296': [['info', 'v2.2 -> v2.3: Qingque Hair LightMap Hash'], ['upgrade_hash', { 'to': 'ddabcef6' }]],

    'c2559faf': [['info', 'v2.2 -> v2.3: Qingque Head Diffuse Hash'], ['upgrade_hash', { 'to': '5421f07d' }]],


    '55e1b1f8': [['info', 'v2.2 -> v2.3: Qingque Body Draw Hash'], ['upgrade_hash', { 'to': '311daa47' }]],
    'e6160d98': [['info', 'v2.2 -> v2.3: Qingque Body Position Hash'], ['upgrade_hash', { 'to': '82ea1627' }]],
    'cc2db614': [['info', 'v2.2 -> v2.3: Qingque Body Texcoord Hash'], ['upgrade_hash', { 'to': 'd97fd893' }]],
    '0a82ceb7': [['info', 'v2.2 -> v2.3: Qingque Body IB Hash'], ['upgrade_hash', { 'to': '21856dc2' }]],

    'ff995bd0': [['info', 'v2.2 -> v2.3: Qingque BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'd92826b3' }]],
    '2d563efe': [['info', 'v2.2 -> v2.3: Qingque BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'a85d8219' }]],
    '149c086c': [['info', 'v2.2 -> v2.3: Qingque BodyC Diffuse Hash'], ['upgrade_hash', { 'to': '92c74827' }]],
    '2b135afe': [['info', 'v2.2 -> v2.3: Qingque BodyC LightMap Hash'], ['upgrade_hash', { 'to': 'f57f3990' }]],

    '109719da': [['info', 'v3.0 -> v3.1: Qingque Body Blend Hash'], ['upgrade_hash', { 'to': 'ff66678e' }]],
    '1efcf666': [['info', 'v3.0 -> v3.1: Qingque Hair Blend Hash'], ['upgrade_hash', { 'to': 'f10d8832' }]],
    '926cd87e': [['info', 'v3.0 -> v3.1: Qingque Hair Position Hash'], ['upgrade_hash', { 'to': '8bd9c5cc' }]],
    '8c6fc97d': [['info', 'v3.0 -> v3.1: Qingque Head Blend Hash'], ['upgrade_hash', { 'to': '639eb729' }]],
    '968fce83': [['info', 'v3.0 -> v3.1: Qingque Head Position Hash'], ['upgrade_hash', { 'to': '8f3ad331' }]],

    '311daa47': [['info', 'v2.2 -> v3.2: Qingque Body Draw Hash'], ['upgrade_hash', { 'to': '3e8e9f4a' }]],
    '82ea1627': [['info', 'v2.3 -> v3.2: Qingque Body Position Hash'], ['upgrade_hash', { 'to': '8020c78e' }]],
    '77ba9951': [['info', 'v3.1 -> v3.2: Qingque Hair Draw Hash'], ['upgrade_hash', { 'to': '7829ac5c' }]],
    'c997b5ee': [['info', 'v3.1 -> v3.2: Qingque Head Draw Hash'], ['upgrade_hash', { 'to': 'c60480e3' }]],


    // MARK: Rapa
    '929ed561': [['info', 'v3.0 -> v3.1: Rappa Body Blend Hash'], ['upgrade_hash', { 'to': '7d6fab35' }]],
    '16da2868': [['info', 'v3.0 -> v3.1: Rappa Hair Blend Hash'], ['upgrade_hash', { 'to': 'f92b563c' }]],
    'd5d249db': [['info', 'v3.0 -> v3.1: Rappa Hair Position Hash'], ['upgrade_hash', { 'to': 'cc675469' }]],
    'be6516d2': [['info', 'v3.0 -> v3.1: Rappa Head Blend Hash'], ['upgrade_hash', { 'to': '51946886' }]],
    '19526add': [['info', 'v3.0 -> v3.1: Rappa Head Position Hash'], ['upgrade_hash', { 'to': '00e7776f' }]],

    '20eae8c1': [['info', 'v3.1 -> v3.2: Rappa Hair Draw Hash'], ['upgrade_hash', { 'to': '2f79ddcc' }]],
    '4004b137': [['info', 'v3.1 -> v3.2: Rappa Head Draw Hash'], ['upgrade_hash', { 'to': '4f97843a' }]],
    'f4af28d6': [['info', 'v3.1 -> v3.2: Rappa Body Draw Hash'], ['upgrade_hash', { 'to': 'fb3c1ddb' }]],
    '475894b6': [['info', 'v3.1 -> v3.2: Rappa Body Position Hash'], ['upgrade_hash', { 'to': '92db0310' }]],


    // MARK: Robin
    '490e6507': [['info', 'v2.2 -> v2.3: Robin HairA Diffuse Hash'], ['upgrade_hash', { 'to': 'b7d76947' }]],
    '63aafaed': [['info', 'v2.2 -> v2.3: Robin HairA LightMap Hash'], ['upgrade_hash', { 'to': '445abbfc' }]],

    '07fd3ce1': [['info', 'v2.2 -> v2.3: Robin HeadA Diffuse Hash'], ['upgrade_hash', { 'to': '14116af5' }]],

    '312e2c95': [['info', 'v2.2 -> v2.3: Robin BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'de39f5f2' }]],
    '4c249936': [['info', 'v2.2 -> v2.3: Robin BodyA LightMap Hash'], ['upgrade_hash', { 'to': '57ba7e2a' }]],

    '9e6b5969': [['info', 'v2.2 -> v2.3: Robin BodyB StarrySkyMask Hash'], ['upgrade_hash', { 'to': 'e5ed0f89' }]],

    'ef273dac': [['info', 'v2.5 -> v2.6: Robin Body Position Hash'], ['upgrade_hash', { 'to': '22e9e92a' }]],
    '43c5c007': [['info', 'v2.5 -> v2.6: Robin Body Texcoord Hash'], ['upgrade_hash', { 'to': 'a65193dc' }]],

    '94a0e452': [['info', 'v3.0 -> v3.1: Robin Body Blend Hash'], ['upgrade_hash', { 'to': '7b519a06' }]],
    '22e9e92a': [['info', 'v2.6 -> v3.1: Robin Body Position Hash'], ['upgrade_hash', { 'to': '3b5cf498' }]],
    '022d66fa': [['info', 'v3.0 -> v3.1: Robin Hair Blend Hash'], ['upgrade_hash', { 'to': 'eddc18ae' }]],
    'c659dc72': [['info', 'v3.0 -> v3.1: Robin Hair Position Hash'], ['upgrade_hash', { 'to': 'dfecc1c0' }]],
    '68e89a79': [['info', 'v3.0 -> v3.1: Robin Head Blend Hash'], ['upgrade_hash', { 'to': '8719e42d' }]],
    '4eb1753f': [['info', 'v3.0 -> v3.1: Robin Head Position Hash'], ['upgrade_hash', { 'to': '5704688d' }]],

    '08627b10': [['info', 'v3.1 -> v3.2: Robin Hair Draw Hash'], ['upgrade_hash', { 'to': '07f14e1d' }]],
    '3e3a2a8a': [['info', 'v3.1 -> v3.2: Robin Head Draw Hash'], ['upgrade_hash', { 'to': '31a91f87' }]],
    '5cd081cc': [['info', 'v3.1 -> v3.2: Robin Body Draw Hash'], ['upgrade_hash', { 'to': '5343b4c1' }]],


    // MARK: RuanMei
    '6f3b9090': [['info', 'v1.6 -> v2.0: RuanMei Body Texcoord Hash'], ['upgrade_hash', { 'to': '803d3eda' }]],
    '35bf6c19': [['info', 'v1.6 -> v2.0: RuanMei BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'fe8145b1' }]],
    'c984b1e6': [['info', 'v1.6 -> v2.0: RuanMei BodyA LightMap Hash'], ['upgrade_hash', { 'to': '9b63577a' }]],
    // 'ab4af2cb': [
    //     ['info', 'v2.1: RuanMei Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '7e4f7890'}],
    //     ['multiply_section', {
    //         'titles': ['RuanMeiBodyPosition', 'RuanMeiBodyPosition_Extra'],
    //         'hashes': ['ab4af2cb', '7e4f7890']
    //     }]
    // ],

    '7e4f7890': [['info', 'v2.2 -> v3.1: RuanMei Body Position Extra Hash'], ['comment_sections', {}]],
    'f6491dae': [['info', 'v2.2 -> v2.3: RuanMei HairA Diffuse Hash'], ['upgrade_hash', { 'to': '22e8a12f' }]],
    '45e0fe2c': [['info', 'v2.2 -> v2.3: RuanMei HairA LightMap Hash'], ['upgrade_hash', { 'to': '0198e0df' }]],

    'b3ddcd02': [['info', 'v2.2 -> v2.3: RuanMei HeadA Diffuse Hash'], ['upgrade_hash', { 'to': 'fd3d44f8' }]],

    'fe8145b1': [['info', 'v2.2 -> v2.3: RuanMei BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '5387a03e' }]],
    '9b63577a': [['info', 'v2.2 -> v2.3: RuanMei BodyA LightMap Hash'], ['upgrade_hash', { 'to': '93eec3ab' }]],

    '2d3d915a': [['info', 'v3.0 -> v3.1: RuanMei Body Blend Hash'], ['upgrade_hash', { 'to': 'c2ccef0e' }]],
    '3b0cb896': [['info', 'v3.0 -> v3.1: RuanMei Hair Blend Hash'], ['upgrade_hash', { 'to': 'd4fdc6c2' }]],
    '7ed84bde': [['info', 'v3.0 -> v3.1: RuanMei Hair Position Hash'], ['upgrade_hash', { 'to': '676d566c' }]],
    '490a691b': [['info', 'v3.0 -> v3.1: RuanMei Head Blend Hash'], ['upgrade_hash', { 'to': 'a6fb174f' }]],
    '5a981b24': [['info', 'v3.0 -> v3.1: RuanMei Head Position Hash'], ['upgrade_hash', { 'to': '432d0696' }]],
    'ab4af2cb': [['info', 'v3.0 -> v3.1: RuanMei Body Position Hash'], ['upgrade_hash', { 'to': '67fa6522' }]],

    '921846ab': [['info', 'v3.1 -> v3.2: RuanMei Head Draw Hash'], ['upgrade_hash', { 'to': '9d8b73a6' }]],
    '18bd4eab': [['info', 'v3.1 -> v3.2: RuanMei Body Draw Hash'], ['upgrade_hash', { 'to': '172e7ba6' }]],



    // MARK: Sampo
    '75824b32': [['info', 'v2.2 -> v2.3: Sampo Hair Draw Hash'], ['upgrade_hash', { 'to': '31447b51' }]],
    'e07731c5': [['info', 'v2.2 -> v2.3: Sampo Hair Position Hash'], ['upgrade_hash', { 'to': '3095786c' }]],
    '529994b6': [['info', 'v2.2 -> v2.3: Sampo Hair Texcoord Hash'], ['upgrade_hash', { 'to': '5974af55' }]],
    'd2e6ad9b': [['info', 'v2.2 -> v2.3: Sampo Hair IB Hash'], ['upgrade_hash', { 'to': '96243edc' }]],
    'ec28a787': [['info', 'v2.2 -> v2.3: Sampo Hair Diffuse Hash'], ['upgrade_hash', { 'to': '36d62e76' }]],
    '22c6ec2c': [['info', 'v2.2 -> v2.3: Sampo Hair LightMap Hash'], ['upgrade_hash', { 'to': '989a13bb' }]],

    '3095d3d1': [['info', 'v2.2 -> v2.3: Sampo Head Diffuse Hash'], ['upgrade_hash', { 'to': '4c904279' }]],

    'a81589e4': [['info', 'v2.2 -> v2.3: Sampo Body Texcoord Hash'], ['upgrade_hash', { 'to': 'e0274b6f' }]],
    '3ac42f7d': [
        ['info', 'v2.2 -> v2.3: Sampo Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'SampoBody',
            'hash': '15761df0',
            'trg_indices': ['0', '20655'],
            'src_indices': ['0', '20637'],
        }]
    ],
    '85c01194': [['info', 'v2.2 -> v2.3: Sampo BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '297b7f7c' }]],
    'e15ccf04': [['info', 'v2.2 -> v2.3: Sampo BodyA LightMap Hash'], ['upgrade_hash', { 'to': '1251e25b' }]],
    '92065503': [['info', 'v2.2 -> v2.3: Sampo BodyB Diffuse Hash'], ['upgrade_hash', { 'to': '4fd99756' }]],
    '333b2634': [['info', 'v2.2 -> v2.3: Sampo BodyB LightMap Hash'], ['upgrade_hash', { 'to': '992d119f' }]],

    'b9371b3c': [['info', 'v3.0 -> v3.1: Sampo Body Blend Hash'], ['upgrade_hash', { 'to': '56c66568' }]],
    'd4ab62d7': [['info', 'v3.0 -> v3.1: Sampo Hair Blend Hash'], ['upgrade_hash', { 'to': '3b5a1c83' }]],
    '3095786c': [['info', 'v2.3 -> v3.1: Sampo Hair Position Hash'], ['upgrade_hash', { 'to': '292065de' }]],
    'f3f0c980': [['info', 'v3.0 -> v3.1: Sampo Head Blend Hash'], ['upgrade_hash', { 'to': '1c01b7d4' }]],
    '3b4bdc1f': [['info', 'v3.0 -> v3.1: Sampo Head Position Hash'], ['upgrade_hash', { 'to': '22fec1ad' }]],

    '31447b51': [['info', 'v2.3 -> v3.2: Sampo Hair Draw Hash'], ['upgrade_hash', { 'to': '3ed74e5c' }]],
    '6656406f': [['info', 'v3.1 -> v3.2: Sampo Head Draw Hash'], ['upgrade_hash', { 'to': '69c57562' }]],
    '8d3b5179': [['info', 'v3.1 -> v3.2: Sampo Body Draw Hash'], ['upgrade_hash', { 'to': '82a86474' }]],
    '3ecced19': [['info', 'v3.1 -> v3.2: Sampo Body Position Hash'], ['upgrade_hash', { 'to': '2087b78d' }]],


    // MARK: Seele
    '41943cc6': [['info', 'v1.6 -> v2.0: Seele Body Texcoord Hash'], ['upgrade_hash', { 'to': 'fe54239f' }]],
    'eb699635': [
        ['info', 'v1.6 -> v2.0: Seele Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'SeeleBody',
            'hash': '6a522a54',
            'trg_indices': ['0', '11550', '19968', '49764'],
            'src_indices': ['0', '-1', '19968', '-1'],
        }]
    ],

    '8f3bec58': [['info', 'v2.2 -> v2.3: Seele HairA Diffuse Hash'], ['upgrade_hash', { 'to': 'ebc707dd' }]],
    '4122931f': [['info', 'v2.2 -> v2.3: Seele HairA LightMap Hash'], ['upgrade_hash', { 'to': 'da303c25' }]],

    'ef4ec36c': [['info', 'v2.2 -> v2.3: Seele HeadA Diffuse Hash'], ['upgrade_hash', { 'to': '75263a6e' }]],

    'fe54239f': [['info', 'v2.2 -> v2.3: Seele Body Texcoord Hash'], ['upgrade_hash', { 'to': '17cba38d' }]],

    '8daeb19c': [['info', 'v2.2 -> v2.3: Seele BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '600c3a12' }]],
    'b06965df': [['info', 'v2.2 -> v2.3: Seele BodyA LightMap Hash'], ['upgrade_hash', { 'to': '14bb544b' }]],
    '1747ac60': [['info', 'v2.2 -> v2.3: Seele BodyC Diffuse Hash'], ['upgrade_hash', { 'to': '8e550df4' }]],
    '32df70e0': [['info', 'v2.2 -> v2.3: Seele BodyC LightMap Hash'], ['upgrade_hash', { 'to': 'c6db3a14' }]],

    '8a952009': [['info', 'v3.0 -> v3.1: Seele Body Blend Hash'], ['upgrade_hash', { 'to': '65645e5d' }]],
    'a9013cae': [['info', 'v3.0 -> v3.1: Seele Hair Blend Hash'], ['upgrade_hash', { 'to': '46f042fa' }]],
    '0eec1b57': [['info', 'v3.0 -> v3.1: Seele Hair Position Hash'], ['upgrade_hash', { 'to': '175906e5' }]],
    'e9ffad95': [['info', 'v3.0 -> v3.1: Seele Head Blend Hash'], ['upgrade_hash', { 'to': '060ed3c1' }]],
    'e1a2635f': [['info', 'v3.0 -> v3.1: Seele Head Position Hash'], ['upgrade_hash', { 'to': 'f8177eed' }]],

    'c6e91591': [['info', 'v3.1 -> v3.2: Seele Hair Draw Hash'], ['upgrade_hash', { 'to': 'c97a209c' }]],
    '4bf88970': [['info', 'v3.1 -> v3.2: Seele Head Draw Hash'], ['upgrade_hash', { 'to': '446bbc7d' }]],
    '45f1986e': [['info', 'v3.1 -> v3.2: Seele Body Draw Hash'], ['upgrade_hash', { 'to': '4a62ad63' }]],
    'f606240e': [['info', 'v3.1 -> v3.2: Seele Body Position Hash'], ['upgrade_hash', { 'to': 'bb8ec098' }]],


    // MARK: Serval
    'c71fc0d0': [['info', 'v1.6 -> v2.0: Serval Body Position Extra Hash'], ['upgrade_hash', { 'to': '1bdfe263' }]],
    '35e3d214': [['info', 'v1.6 -> v2.0: Serval Body Texcoord Hash'], ['upgrade_hash', { 'to': '86d77809' }]],
    '44885792': [
        ['info', 'v1.6 -> v2.0: Serval Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'ServalBody',
            'hash': 'f092876d',
            'trg_indices': ['0', '13731', '30048', '58380'],
            'src_indices': ['-1', '0', '30048', '-1'],
        }]
    ],
    // '383717ed': [
    //     ['info', 'v2.1: Serval Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '1bdfe263'}],
    //     ['check_hash_not_in_ini', {'hash': 'c71fc0d0'}],
    //     ['multiply_section', {
    //         'titles': ['ServalBodyPosition', 'ServalBodyPosition_Extra'],
    //         'hashes': ['383717ed', '1bdfe263']
    //     }]
    // ],

    '59d7157b': [['info', 'v2.2 -> v2.3: Serval HairA Diffuse Hash'], ['upgrade_hash', { 'to': '21e4c3cd' }]],
    '86144243': [['info', 'v2.2 -> v2.3: Serval HairA LightMap Hash'], ['upgrade_hash', { 'to': '79709858' }]],

    'd00782c7': [['info', 'v2.2 -> v2.3: Serval HeadA Diffuse Hash'], ['upgrade_hash', { 'to': 'afd4f008' }]],

    '8d159053': [['info', 'v2.2 -> v2.3: Serval BodyB Diffuse Hash'], ['upgrade_hash', { 'to': '1bc2fa5f' }]],
    '7e8fa12b': [['info', 'v2.2 -> v2.3: Serval BodyB LightMap Hash'], ['upgrade_hash', { 'to': 'a05979e4' }]],
    '6efdb42c': [['info', 'v2.2 -> v2.3: Serval BodyB StockingMap Hash'], ['upgrade_hash', { 'to': 'c7358fb2' }]],
    '269745d0': [['info', 'v2.2 -> v2.3: Serval BodyC Diffuse Hash'], ['upgrade_hash', { 'to': '5be64601' }]],
    '725d36ab': [['info', 'v2.2 -> v2.3: Serval BodyC LightMap Hash'], ['upgrade_hash', { 'to': 'c7bd5694' }]],

    'ab709ef7': [['info', 'v3.0 -> v3.1: Serval Body Blend Hash'], ['upgrade_hash', { 'to': '4481e0a3' }]],
    '1bdfe263': [['info', 'v2.0 -> v3.1: Serval Body Position Extra Hash'], ['comment_sections', {}]],
    '5bca39c4': [['info', 'v3.0 -> v3.1: Serval Hair Blend Hash'], ['upgrade_hash', { 'to': 'b43b4790' }]],
    '591d16e1': [['info', 'v3.0 -> v3.1: Serval Hair Position Hash'], ['upgrade_hash', { 'to': '40a80b53' }]],
    '876b2a8c': [['info', 'v3.0 -> v3.1: Serval Head Blend Hash'], ['upgrade_hash', { 'to': '689a54d8' }]],
    '2de93d4b': [['info', 'v3.0 -> v3.1: Serval Head Position Hash'], ['upgrade_hash', { 'to': '345c20f9' }]],
    '383717ed': [['info', 'v3.0 -> v3.1: Serval Body Position Hash'], ['upgrade_hash', { 'to': '026affd1' }]],

    '403c9fe7': [['info', 'v3.1 -> v3.2: Serval Hair Draw Hash'], ['upgrade_hash', { 'to': '4fafaaea' }]],
    '8bc0ab8d': [['info', 'v3.1 -> v3.2: Serval Body Draw Hash'], ['upgrade_hash', { 'to': '84539e80' }]],



    // MARK: Sparkle
    // SCYLL SAID NOT TO TOUCH HER
    '28788045': [['info', 'v2.0 -> v2.1: Sparkle Body Texcoord Hash'], ['upgrade_hash', { 'to': 'd51f3972' }]],
    '74660eca': [['info', 'v2.0 -> v2.1: Sparkle Body IB Hash'], ['upgrade_hash', { 'to': '68121fd3' }]],

    '3c22971b': [['info', 'v2.1 -> v2.2: Sparkle BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'fac7d488' }]],

    '1d7ed602': [['info', 'v2.2 -> v2.3: Sparkle Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'a4f91fac' }]],
    '07b2e4b7': [['info', 'v2.2 -> v2.3: Sparkle Hair LightMap Hash'], ['upgrade_hash', { 'to': 'df96b015' }]],

    '6594fbb2': [['info', 'v2.2 -> v2.3: Sparkle Head Diffuse Hash'], ['upgrade_hash', { 'to': '09733ebc' }]],

    'fac7d488': [['info', 'v2.2 -> v2.3: Sparkle Body Diffuse Hash'], ['upgrade_hash', { 'to': '17999c91' }]],
    'a4974a51': [['info', 'v2.2 -> v2.3: Sparkle Body LightMap Hash'], ['upgrade_hash', { 'to': 'f806d2e4' }]],

    '91b9fb51': [['info', 'v3.0 -> v3.1: Sparkle Body Blend Hash'], ['upgrade_hash', { 'to': '7e488505' }]],
    '4e477254': [['info', 'v3.0 -> v3.1: Sparkle Hair Blend Hash'], ['upgrade_hash', { 'to': 'a1b60c00' }]],
    '5b9af3ba': [['info', 'v3.0 -> v3.1: Sparkle Hair Position Hash'], ['upgrade_hash', { 'to': '422fee08' }]],
    '4873b590': [['info', 'v3.0 -> v3.1: Sparkle Head Blend Hash'], ['upgrade_hash', { 'to': 'a782cbc4' }]],
    'c2cce86e': [['info', 'v3.0 -> v3.1: Sparkle Head Position Hash'], ['upgrade_hash', { 'to': 'db79f5dc' }]],

    '2ffc74a7': [['info', 'v3.1 -> v3.2: Sparkle Hair Draw Hash'], ['upgrade_hash', { 'to': '206f41aa' }]],
    'b2bc717f': [['info', 'v3.1 -> v3.2: Sparkle Head Draw Hash'], ['upgrade_hash', { 'to': 'bd2f4472' }]],
    'c22dc904': [['info', 'v3.1 -> v3.2: Sparkle Body Draw Hash'], ['upgrade_hash', { 'to': 'cdbefc09' }]],
    '71da7564': [['info', 'v3.1 -> v3.2: Sparkle Body Position Hash'], ['upgrade_hash', { 'to': '9588097c' }]],


    // MARK: SilverWolf
    '429574bd': [['info', 'v1.6 -> v2.0: SilverWolf Body Draw Hash'], ['upgrade_hash', { 'to': '6bb20ea8' }]],
    'f162c8dd': [['info', 'v1.6 -> v2.0: SilverWolf Body Position Hash'], ['upgrade_hash', { 'to': 'd845b2c8' }]],
    '2e053525': [['info', 'v1.6 -> v2.0: SilverWolf Body Texcoord Hash'], ['upgrade_hash', { 'to': 'ab13f8b8' }]],
    '729de5d2': [
        ['info', 'v1.6 -> v2.0: SilverWolf Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'SilverWolfBody',
            'hash': 'e8f10ab3',
            'trg_indices': ['0', '63549', '63663'],
            'src_indices': ['0', '64392', '-1'],
        }]
    ],
    'd28049f2': [
        ['upgrade_shared_hash', {
            'to': 'dd137cff',
            'flag_hashes': ['876b2a8c', '689a54d8', '2de93d4b', '345c20f9'],
            'log_info': 'v3.1 -> v3.2: Serval Head Draw Hash',
        }],
        ['upgrade_shared_hash', {
            'to': '293abc6c',
            'flag_hashes': ['b2d04673', '520314e4', '6f9922fe', 'b9254611'],
            'log_info': 'v2.2 -> v2.3: SilverWolf Hair Draw Hash',
        }],
    ],
    'b2d04673': [['info', 'v2.2 -> v2.3: SilverWolf Hair Position Hash'], ['upgrade_hash', { 'to': '520314e4' }]],
    '6f9922fe': [['info', 'v2.2 -> v2.3: SilverWolf Hair Texcoord Hash'], ['upgrade_hash', { 'to': 'b9254611' }]],
    '3608ba80': [['info', 'v2.2 -> v2.3: SilverWolf Hair IB Hash'], ['upgrade_hash', { 'to': '91db78c2' }]],
    '56893677': [['info', 'v2.2 -> v2.3: SilverWolf Hair Diffuse Hash'], ['upgrade_hash', { 'to': '7c7065ae' }]],
    'dd608b21': [['info', 'v2.2 -> v2.3: SilverWolf Hair LightMap Hash'], ['upgrade_hash', { 'to': 'cf2cb5b7' }]],

    'd99747d7': [['info', 'v2.2 -> v2.3: SilverWolf Head Diffuse Hash'], ['upgrade_hash', { 'to': 'a05a9801' }]],

    'ab13f8b8': [['info', 'v2.2 -> v2.3: SilverWolf Body Texcoord Hash'], ['upgrade_hash', { 'to': '6c945131' }]],
    'e8f10ab3': [
        ['info', 'v2.2 -> v2.3: SilverWolf Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'SilverWolfBody',
            'hash': '891ecaae',
            'trg_indices': ['0', '63429', '63543'],
            'src_indices': ['0', '63549', '63663'],
        }]
    ],
    '76d6dd31': [['info', 'v2.2 -> v2.3: SilverWolf Body Diffuse Hash'], ['upgrade_hash', { 'to': 'b2f97e36' }]],
    '84b3170b': [['info', 'v2.2 -> v2.3: SilverWolf Body LightMap Hash'], ['upgrade_hash', { 'to': '7b1eface' }]],

    '17906457': [['info', 'v3.0 -> v3.1: SilverWolf Body Blend Hash'], ['upgrade_hash', { 'to': 'f8611a03' }]],
    '567d08be': [['info', 'v3.0 -> v3.1: SilverWolf Hair Blend Hash'], ['upgrade_hash', { 'to': 'b98c76ea' }]],
    '520314e4': [['info', 'v2.3 -> v3.1: SilverWolf Hair Position Hash'], ['upgrade_hash', { 'to': '4bb60956' }]],
    '2cf46858': [['info', 'v3.0 -> v3.1: SilverWolf Head Blend Hash'], ['upgrade_hash', { 'to': 'c305160c' }]],
    '314115a3': [['info', 'v3.0 -> v3.1: SilverWolf Head Position Hash'], ['upgrade_hash', { 'to': '28f40811' }]],

    '6bb20ea8': [['info', 'v2.0 -> v3.2: SilverWolf Body Draw Hash'], ['upgrade_hash', { 'to': '64213ba5' }]],
    'd845b2c8': [['info', 'v2.0 -> v3.2: SilverWolf Body Position Hash'], ['upgrade_hash', { 'to': 'bd2443af' }]],
    '293abc6c': [
        ['upgrade_shared_hash', {
            'to': '26a98961',
            'flag_hashes': ['567d08be', '3608ba80', '91db78c2', 'b98c76ea'],
            'log_info': 'v2.3 -> v3.2: Silver Wolf Hair Draw Hash',
        }],
    ],


    // MARK: Sunday
    // 'acc75a16': [
    //     ['info', 'v2.7: Sunday Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '2e207a71'}],
    //     ['multiply_section', {
    //         'titles': ['SundayBodyPosition', 'SundayBodyPosition_Extra'],
    //         'hashes': ['acc75a16', '2e207a71']
    //     }]
    // ],
    '7220563b': [['info', 'v2.7 -> v3.0: Sunday Body Diffuse Hash'], ['upgrade_hash', { 'to': '5b056989' }]],
    '9ec5fcc5': [['info', 'v2.7 -> v3.0: Sunday Body Diffuse Hash'], ['upgrade_hash', { 'to': '09fdcc99' }]],
    '2e207a71': [['info', 'v2.7 -> v3.1: Sunday Body Position Extra Hash'], ['comment_sections', {}]],
    'acc75a16': [['info', 'v2.7 -> v3.1: Sunday Body Position Hash'], ['upgrade_hash', { 'to': '379567c3' }]],
    'c5c1150d': [['info', 'v3.0 -> v3.1: Sunday Body Blend Hash'], ['upgrade_hash', { 'to': '2a306b59' }]],
    'ca09a4f1': [['info', 'v3.0 -> v3.1: Sunday Hair Position Hash'], ['upgrade_hash', { 'to': 'd3bcb943' }]],
    'b03f76bb': [['info', 'v3.0 -> v3.1: Sunday Book Position Hash'], ['upgrade_hash', { 'to': 'a98a6b09' }]],
    'f74c7344': [['info', 'v3.0 -> v3.1: Sunday Hair Blend Hash'], ['upgrade_hash', { 'to': '18bd0d10' }]],
    '0fc6e4fe': [['info', 'v3.0 -> v3.1: Sunday Face Position Hash'], ['upgrade_hash', { 'to': '1673f94c' }]],

    'a147107a': [['info', 'v3.1 -> v3.2: Sunday Hair Draw Hash'], ['upgrade_hash', { 'to': 'aed42577' }]],
    '86890d04': [['info', 'v3.1 -> v3.2: Sunday Head Draw Hash'], ['upgrade_hash', { 'to': '891a3809' }]],
    '1f30e676': [['info', 'v3.1 -> v3.2: Sunday Body Draw Hash'], ['upgrade_hash', { 'to': '10a3d37b' }]],


    // MARK: Sushang
    '59a0b558': [['info', 'v1.6 -> v2.0: Sushang Body Texcoord Hash'], ['upgrade_hash', { 'to': '23dc010c' }]],
    'd765c517': [
        ['info', 'v1.6 -> v2.0: Sushang Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'SushangBody',
            'hash': '4b22391b',
            'trg_indices': ['0', '3531', '30774', '44049'],
            'src_indices': ['0', '-1', '30774', '-1'],
        }]
    ],

    '95e614e5': [['info', 'v2.2 -> v2.3: Sushang Hair Diffuse Hash'], ['upgrade_hash', { 'to': '636dc89e' }]],
    '728565ee': [['info', 'v2.2 -> v2.3: Sushang Hair LightMap Hash'], ['upgrade_hash', { 'to': '0e484aa5' }]],

    '9d7ea82f': [['info', 'v2.2 -> v2.3: Sushang Head Diffuse Hash'], ['upgrade_hash', { 'to': '1897cfee' }]],

    'e4ccda3f': [['info', 'v2.2 -> v2.3: Sushang BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '98507746' }]],
    '653b35cd': [['info', 'v2.2 -> v2.3: Sushang BodyA LightMap Hash'], ['upgrade_hash', { 'to': '3134e1e4' }]],
    '4724e9c1': [['info', 'v2.2 -> v2.3: Sushang BodyC Diffuse Hash'], ['upgrade_hash', { 'to': '79354f80' }]],
    'd2e9d4dc': [['info', 'v2.2 -> v2.3: Sushang BodyC LightMap Hash'], ['upgrade_hash', { 'to': '1e9893b3' }]],

    'f30f2d7f': [['info', 'v3.0 -> v3.1: Sushang Body Blend Hash'], ['upgrade_hash', { 'to': '1cfe532b' }]],
    '38293906': [['info', 'v3.0 -> v3.1: Sushang Hair Blend Hash'], ['upgrade_hash', { 'to': 'd7d84752' }]],
    'c87cf153': [['info', 'v3.0 -> v3.1: Sushang Hair Position Hash'], ['upgrade_hash', { 'to': 'd1c9ece1' }]],
    'd0568383': [['info', 'v3.0 -> v3.1: Sushang Head Blend Hash'], ['upgrade_hash', { 'to': '3fa7fdd7' }]],
    'ad69421b': [['info', 'v3.0 -> v3.1: Sushang Head Position Hash'], ['upgrade_hash', { 'to': 'b4dc5fa9' }]],

    '86eff764': [['info', 'v3.1 -> v3.2: Sushang Hair Draw Hash'], ['upgrade_hash', { 'to': '897cc269' }]],
    'eccf68bc': [['info', 'v3.1 -> v3.2: Sushang Head Draw Hash'], ['upgrade_hash', { 'to': 'e35c5db1' }]],
    '7f1e869c': [['info', 'v3.1 -> v3.2: Sushang Body Draw Hash'], ['upgrade_hash', { 'to': '708db391' }]],
    'cce93afc': [['info', 'v3.1 -> v3.2: Sushang Body Position Hash'], ['upgrade_hash', { 'to': 'df4128f2' }]],


    // MARK: The Herta
    '2279c5a6': [['info', 'v3.1 -> v3.2: The Herta Hair Draw Hash'], ['upgrade_hash', { 'to': '2deaf0ab' }]],
    'f9199c35': [['info', 'v3.1 -> v3.2: The Herta Head Draw Hash'], ['upgrade_hash', { 'to': 'f68aa938' }]],
    '1a199989': [['info', 'v3.1 -> v3.2: The Herta Body Draw Hash'], ['upgrade_hash', { 'to': '158aac84' }]],


    // MARK: Tingyun
    '1870a9cb': [['info', 'v1.4 -> v1.6: Tingyun BodyA LightMap Hash'], ['upgrade_hash', { 'to': '547497fb' }]],
    '6e205d4e': [['info', 'v1.4 -> v1.6: Tingyun BodyB LightMap Hash'], ['upgrade_hash', { 'to': '73fad5f5' }]],
    '9bf82eaa': [['info', 'v1.6 -> v2.0: Tingyun Body Texcoord Hash'], ['upgrade_hash', { 'to': 'f83ec867' }]],
    '351d8570': [
        ['info', 'v1.6 -> v2.0: Tingyun Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'TingyunBody',
            'hash': 'da59600b',
            'trg_indices': ['0', '10905', '53229', '54588', '59736'],
            'src_indices': ['0', '16053', '-1', '-1', '59736'],
        }]
    ],

    '02a81179': [['info', 'v2.2 -> v2.3: Tingyun Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'c4be701a' }]],
    'fa9143b8': [['info', 'v2.2 -> v2.3: Tingyun Hair LightMap Hash'], ['upgrade_hash', { 'to': 'f699e83b' }]],

    'bdfd3d71': [['info', 'v2.2 -> v2.3: Tingyun Head Diffuse Hash'], ['upgrade_hash', { 'to': 'fb95c111' }]],

    '77ddf35c': [['info', 'v2.2 -> v2.3: Tingyun BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'ed473e73' }]],
    '547497fb': [['info', 'v2.2 -> v2.3: Tingyun BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'e0fa7d8e' }]],
    '1cbf0500': [['info', 'v2.2 -> v2.3: Tingyun BodyC Diffuse Hash'], ['upgrade_hash', { 'to': 'bf7501ab' }]],
    '73fad5f5': [['info', 'v2.2 -> v2.3: Tingyun BodyC LightMap Hash'], ['upgrade_hash', { 'to': 'fa54a59b' }]],

    '069ee84c': [['info', 'v3.0 -> v3.1: Tingyun Body Blend Hash'], ['upgrade_hash', { 'to': 'e96f9618' }]],
    '44cee658': [['info', 'v3.0 -> v3.1: Tingyun Hair Blend Hash'], ['upgrade_hash', { 'to': 'ab3f980c' }]],
    'be07554f': [['info', 'v3.0 -> v3.1: Tingyun Hair Position Hash'], ['upgrade_hash', { 'to': 'a7b248fd' }]],
    'ff6fdae4': [['info', 'v3.0 -> v3.1: Tingyun Head Blend Hash'], ['upgrade_hash', { 'to': '109ea4b0' }]],
    'f9fa713f': [['info', 'v3.0 -> v3.1: Tingyun Head Position Hash'], ['upgrade_hash', { 'to': 'e04f6c8d' }]],

    '0d41cc95': [['info', 'v3.1 -> v3.2: Tingyun Hair Draw Hash'], ['upgrade_hash', { 'to': '02d2f998' }]],
    '7d4f8bae': [['info', 'v3.1 -> v3.2: Tingyun Head Draw Hash'], ['upgrade_hash', { 'to': '72dcbea3' }]],
    '6a4333dc': [['info', 'v3.1 -> v3.2: Tingyun Body Draw Hash'], ['upgrade_hash', { 'to': '65d006d1' }]],
    'd9b48fbc': [['info', 'v3.1 -> v3.2: Tingyun Body Position Hash'], ['upgrade_hash', { 'to': 'b061758a' }]],


    // MARK: Topaz
    '6f354853': [['info', 'v1.6 -> v2.0: Topaz Body Position Extra Hash'], ['upgrade_hash', { 'to': '71d39d95' }]],
    '24212bf6': [['info', 'v1.6 -> v2.0: Topaz Body Texcoord Hash'], ['upgrade_hash', { 'to': '436288c9' }]],
    'ae42518c': [
        ['info', 'v1.6 -> v2.0: Topaz Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'TopazBody',
            'hash': 'b52297bf',
            'trg_indices': ['0', '18327', '21645', '45078'],
            'src_indices': ['0', '-1', '21645', '-1'],
        }]
    ],
    // '2eab6d2d': [
    //     ['info', 'v2.1: Topaz Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '71d39d95'}],
    //     ['check_hash_not_in_ini', {'hash': '6f354853'}],
    //     ['multiply_section', {
    //         'titles': ['TopazBodyPosition', 'TopazBodyPosition_Extra'],
    //         'hashes': ['2eab6d2d', '71d39d95']
    //     }]
    // ],
    '71d39d95': [['info', 'v2.2 -> v3.1: Topaz Body Position Extra Hash'], ['comment_sections', {}]],
    '0dd40a0b': [['info', 'v2.2 -> v2.3: Topaz Hair Draw Hash'], ['upgrade_hash', { 'to': 'cc870789' }]],
    '7fac28de': [['info', 'v2.2 -> v2.3: Topaz Hair Position Hash'], ['upgrade_hash', { 'to': 'a413be23' }]],
    'b8ec605d': [['info', 'v2.2 -> v2.3: Topaz Hair Texcoord Hash'], ['upgrade_hash', { 'to': 'b131f866' }]],
    'f1a4401b': [['info', 'v2.2 -> v2.3: Topaz Hair IB Hash'], ['upgrade_hash', { 'to': '32ef4b75' }]],
    '943bf9d3': [['info', 'v2.2 -> v2.3: Topaz Hair Diffuse Hash'], ['upgrade_hash', { 'to': '78059f75' }]],
    '67df29ec': [['info', 'v2.2 -> v2.3: Topaz Hair LightMap Hash'], ['upgrade_hash', { 'to': '39fd4ba7' }]],

    'fea9fff4': [['info', 'v2.2 -> v2.3: Topaz Head Diffuse Hash'], ['upgrade_hash', { 'to': 'fc521095' }]],

    '436288c9': [['info', 'v2.2 -> v2.3: Topaz Body Texcoord Hash'], ['upgrade_hash', { 'to': '4be08333' }]],
    '96f5e350': [['info', 'v2.2 -> v2.3: Topaz BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '3dfd62b8' }]],
    '6a0ee180': [['info', 'v2.2 -> v2.3: Topaz BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'b8c954ef' }]],
    '68b887db': [['info', 'v2.2 -> v2.3: Topaz BodyC Diffuse Hash'], ['upgrade_hash', { 'to': '13be2437' }]],
    '924edd3e': [['info', 'v2.2 -> v2.3: Topaz BodyC LightMap Hash'], ['upgrade_hash', { 'to': '786f6565' }]],

    '55ef95d4': [['info', 'v3.0 -> v3.1: Topaz Body Blend Hash'], ['upgrade_hash', { 'to': 'ba1eeb80' }]],
    'cbd71321': [['info', 'v3.0 -> v3.1: Topaz Hair Blend Hash'], ['upgrade_hash', { 'to': '24266d75' }]],
    'a413be23': [['info', 'v2.3 -> v3.1: Topaz Hair Position Hash'], ['upgrade_hash', { 'to': 'bda6a391' }]],
    '78012798': [['info', 'v3.0 -> v3.1: Topaz Head Blend Hash'], ['upgrade_hash', { 'to': '97f059cc' }]],
    'e0b28d05': [['info', 'v3.0 -> v3.1: Topaz Head Position Hash'], ['upgrade_hash', { 'to': 'f90790b7' }]],
    '2eab6d2d': [['info', 'v3.1 -> v3.2: Topaz Body Position Hash'], ['upgrade_hash', { 'to': '68668027' }]],

    'cc870789': [['info', 'v2.3 -> v3.2: Topaz Hair Draw Hash'], ['upgrade_hash', { 'to': 'c3143284' }]],
    '7457372c': [['info', 'v3.1 -> v3.2: Topaz Head Draw Hash'], ['upgrade_hash', { 'to': '7bc40221' }]],
    '9d5cd14d': [['info', 'v3.1 -> v3.2: Topaz Body Draw Hash'], ['upgrade_hash', { 'to': '92cfe440' }]],

    // MARK: Tribbie
    'de857b6f': [['info', 'v3.1 -> v3.2: Tribbie Hair Draw Hash'], ['upgrade_hash', { 'to': 'd1164e62' }]],
    '36f9ddcb': [['info', 'v3.1 -> v3.2: Tribbie Head Draw Hash'], ['upgrade_hash', { 'to': '396ae8c6' }]],
    '90844efd': [['info', 'v3.1 -> v3.2: Tribbie Body Draw Hash'], ['upgrade_hash', { 'to': '9f177bf0' }]],
    'cc339c92': [['info', 'v3.1 -> v3.2: Trianne Head Draw Hash'], ['upgrade_hash', { 'to': 'c3a0a99f' }]],
    'f29b3fab': [['info', 'v3.1 -> v3.2: Trinnon Head Draw Hash'], ['upgrade_hash', { 'to': 'fd080aa6' }]],

    // MARK: Welt
    'cb4839db': [['info', 'v1.6 -> v2.0: Welt HairA LightMap Hash'], ['upgrade_hash', { 'to': '2258cc03' }]],
    'fef626ce': [['info', 'v1.6 -> v2.0: Welt Body Position Hash'], ['upgrade_hash', { 'to': '31c9604b' }]],
    '723e0365': [
        ['info', 'v1.6 -> v2.0: Welt Body Texcoord Hash + Buffer add Texcoord1'],
        ['modify_buffer', {
            'operation': 'add_texcoord1',
            'payload': {
                'format': '<BBBBee',
                'value': 'copy'
            }
        }],
        ['upgrade_hash', { 'to': '0ab3a636' }]
    ],
    '374ac8a9': [
        ['info', 'v1.6 -> v2.0: Welt Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'WeltBody',
            'hash': 'd15987b1',
            'trg_indices': ['0', '30588', '46620'],
            'src_indices': ['0', '30588', '-1'],
        }]
    ],
    '1dea5b29': [['info', 'v2.0 -> v2.1: Welt Body Draw Hash'], ['upgrade_hash', { 'to': 'ce076065' }]],
    '31c9604b': [['info', 'v2.0 -> v2.1: Welt Body Position Hash'], ['upgrade_hash', { 'to': '7df0dc05' }]],
    '0ab3a636': [
        ['info', 'v2.0 -> v2.1: Welt Body Texcoord Hash Upgrade + Buffer pad'],
        ['modify_buffer', {
            'operation': 'convert_format',
            'payload': {
                'format_conversion': ['<BBBBeeee', '<BBBBffff']
            }
        }],
        ['upgrade_hash', { 'to': '381a994e' }]
    ],
    'd15987b1': [
        ['info', 'v2.0 -> v2.1: Welt Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'WeltBody',
            'hash': 'e9f71838',
            'trg_indices': ['0', '30588', '48087'],
            'src_indices': ['0', '30588', '46620'],
        }]
    ],
    // '7df0dc05': [
    //     ['info', 'v2.1: Welt Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '5c4ca7f9'}],
    //     ['multiply_section', {
    //         'titles': ['WeltBodyPosition', 'WeltBodyPosition_Extra'],
    //         'hashes': ['7df0dc05', '5c4ca7f9']
    //     }]
    // ],
    '5c4ca7f9': [['info', 'v2.2 -> v3.1: Welt Body Position Extra Hash'], ['comment_sections', {}]],
    '78ca8241': [['info', 'v2.2 -> v2.3: Welt Hair Texcoord Hash'], ['upgrade_hash', { 'to': '8d2fdd4b' }]],
    '6a8dcc20': [['info', 'v2.2 -> v2.3: Welt Hair Diffuse Hash'], ['upgrade_hash', { 'to': '9dd3ae5d' }]],
    '2258cc03': [['info', 'v2.2 -> v2.3: Welt Hair LightMap Hash'], ['upgrade_hash', { 'to': 'c6f7c43c' }]],

    '58db3a4d': [['info', 'v2.2 -> v2.3: Welt Head Diffuse Hash'], ['upgrade_hash', { 'to': 'b4d6d5df' }]],

    'c89a97aa': [['info', 'v2.2 -> v2.3: Welt BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'd318fc3e' }]],
    'b63f51eb': [['info', 'v2.2 -> v2.3: Welt BodyA LightMap Hash'], ['upgrade_hash', { 'to': '8cd33bbc' }]],
    '5c9711f2': [['info', 'v2.2 -> v2.3: Welt BodyB Diffuse Hash'], ['upgrade_hash', { 'to': '948e03bd' }]],
    '3dbb2ae6': [['info', 'v2.2 -> v2.3: Welt BodyB LightMap Hash'], ['upgrade_hash', { 'to': 'd77a2807' }]],

    'aa4229a3': [['info', 'v3.0 -> v3.1: Welt Body Blend Hash'], ['upgrade_hash', { 'to': '45b357f7' }]],
    '34e99315': [['info', 'v3.0 -> v3.1: Welt Hair Blend Hash'], ['upgrade_hash', { 'to': 'db18ed41' }]],
    'c13c202e': [['info', 'v3.0 -> v3.1: Welt Hair Position Hash'], ['upgrade_hash', { 'to': 'd8893d9c' }]],
    '0aafd819': [['info', 'v3.0 -> v3.1: Welt Head Blend Hash'], ['upgrade_hash', { 'to': 'e55ea64d' }]],
    '8cad004f': [['info', 'v3.0 -> v3.1: Welt Head Position Hash'], ['upgrade_hash', { 'to': '95181dfd' }]],
    '7df0dc05': [['info', 'v3.1 -> v3.2: Welt Body Position Hash'], ['upgrade_hash', { 'to': '45f9ba4b' }]],

    'ce076065': [['info', 'v2.1 -> v3.2: Welt Body Draw Hash'], ['upgrade_hash', { 'to': 'c1945568' }]],
    '25f28b95': [['info', 'v3.1 -> v3.2: Welt Hair Draw Hash'], ['upgrade_hash', { 'to': '2a61be98' }]],
    'a6cab8a7': [['info', 'v3.1 -> v3.2: Welt Head Draw Hash'], ['upgrade_hash', { 'to': 'a9598daa' }]],


    // MARK: Xueyi
    '77b78d33': [['info', 'v1.6 -> v2.0: Xueyi Body Position Extra Hash'], ['upgrade_hash', { 'to': '8936451b' }]],
    '2c096545': [['info', 'v1.6 -> v2.0: Xueyi Body Texcoord Hash'], ['upgrade_hash', { 'to': '03ff3d10' }]],
    '9f040cd3': [
        ['info', 'v1.6 -> v2.0: Xueyi Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'XueyiBody',
            'hash': 'af2983dd',
            'trg_indices': ['0', '31986', '39129', '54279'],
            'src_indices': ['0', '-1', '39129', '-1'],
        }]
    ],
    // '206b86f0': [
    //     ['info', 'v2.1: Xueyi Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '8936451b'}],
    //     ['check_hash_not_in_ini', {'hash': '77b78d33'}],
    //     ['multiply_section', {
    //         'titles': ['XueyiBodyPosition', 'XueyiBodyPosition_Extra'],
    //         'hashes': ['206b86f0', '8936451b']
    //     }]
    // ],

    '952c20b8': [['info', 'v2.2 -> v2.3: Xueyi Hair Diffuse Hash'], ['upgrade_hash', { 'to': '360ebd7f' }]],
    'dbb181aa': [['info', 'v2.2 -> v2.3: Xueyi Hair LightMap Hash'], ['upgrade_hash', { 'to': '4d5812b5' }]],

    '3c0e2e71': [['info', 'v2.2 -> v2.3: Xueyi Head Diffuse Hash'], ['upgrade_hash', { 'to': 'f927a99b' }]],

    'ad22f871': [['info', 'v2.2 -> v2.3: Xueyi BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'e2284397' }]],
    '2e328427': [['info', 'v2.2 -> v2.3: Xueyi BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'a694c7ef' }]],
    '957cf6d9': [['info', 'v2.2 -> v2.3: Xueyi BodyC Diffuse Hash'], ['upgrade_hash', { 'to': '89724253' }]],
    '76f171f5': [['info', 'v2.2 -> v2.3: Xueyi BodyC LightMap Hash'], ['upgrade_hash', { 'to': '91c7faef' }]],

    '8936451b': [['info', 'v2.0 -> v3.1: Xueyi Body Position Extra Hash'], ['comment_sections', {}]],
    '9127a7b3': [['info', 'v3.0 -> v3.1: Xueyi Body Blend Hash'], ['upgrade_hash', { 'to': '7ed6d9e7' }]],
    'e0f8ce61': [['info', 'v3.0 -> v3.1: Xueyi Hair Blend Hash'], ['upgrade_hash', { 'to': '0f09b035' }]],
    '5ad7108a': [['info', 'v3.0 -> v3.1: Xueyi Hair Position Hash'], ['upgrade_hash', { 'to': '43620d38' }]],
    '4aab3325': [['info', 'v3.0 -> v3.1: Xueyi Head Blend Hash'], ['upgrade_hash', { 'to': 'a55a4d71' }]],
    'acec2843': [['info', 'v3.0 -> v3.1: Xueyi Head Position Hash'], ['upgrade_hash', { 'to': 'b55935f1' }]],
    '206b86f0': [['info', 'v3.1 -> v3.2: Xueyi Body Position Hash'], ['upgrade_hash', { 'to': '908358a9' }]],

    '4a753694': [['info', 'v3.1 -> v3.2: Xueyi Hair Draw Hash'], ['upgrade_hash', { 'to': '45e60399' }]],
    '2e4a1f92': [['info', 'v3.1 -> v3.2: Xueyi Head Draw Hash'], ['upgrade_hash', { 'to': '21d92a9f' }]],
    '939c3a90': [['info', 'v3.1 -> v3.2: Xueyi Body Draw Hash'], ['upgrade_hash', { 'to': '9c0f0f9d' }]],


    // MARK: Yanqing
    'ef7a4f40': [['info', 'v1.6 -> v2.0: Yanqing Body Position Extra Hash'], ['upgrade_hash', { 'to': 'a09059a0' }]],
    '6fc50cb8': [['info', 'v1.6 -> v2.0: Yanqing Texcoord Hash'], ['upgrade_hash', { 'to': '9801327a' }]],
    'a3fe2b8f': [['info', 'v1.6 -> v2.0: Yanqing BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '4e8f9778' }]],
    'e7e004ca': [['info', 'v1.6 -> v2.0: Yanqing BodyA LightMap Hash'], ['upgrade_hash', { 'to': '035f0719' }]],
    'c20cd648': [
        ['info', 'v1.6 -> v2.0: Yanqing Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'YanqingBody',
            'hash': 'd03803e6',
            'trg_indices': ['0', '55983'],
            'src_indices': ['0', '-1'],
        }]
    ],
    // '5c21b25d': [
    //     ['info', 'v2.1: Yanqing Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': 'a09059a0'}],
    //     ['check_hash_not_in_ini', {'hash': 'ef7a4f40'}],
    //     ['multiply_section', {
    //         'titles': ['YanqingBodyPosition', 'YanqingBodyPosition_Extra'],
    //         'hashes': ['5c21b25d', 'a09059a0']
    //     }]
    // ],

    'a09059a0': [['info', 'v2.2 -> v3.1: Yanqing Body Position Extra Hash'], ['comment_sections', {}]],
    'ea81180d': [['info', 'v2.2 -> v2.3: Yanqing Hair Texcoord Hash'], ['upgrade_hash', { 'to': 'e5457b98' }]],
    '14629990': [['info', 'v2.2 -> v2.3: Yanqing Hair Diffuse Hash'], ['upgrade_hash', { 'to': '541ba63d' }]],
    '0519a715': [['info', 'v2.2 -> v2.3: Yanqing Hair LightMap Hash'], ['upgrade_hash', { 'to': '9639c2cb' }]],

    'af6f0aa8': [['info', 'v2.2 -> v2.3: Yanqing Head Diffuse Hash'], ['upgrade_hash', { 'to': '80763bb9' }]],

    '4e8f9778': [['info', 'v2.2 -> v2.3: Yanqing BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'a41345d3' }]],
    '035f0719': [['info', 'v2.2 -> v2.3: Yanqing BodyA LightMap Hash'], ['upgrade_hash', { 'to': '2db9f1d6' }]],

    '5b021ee9': [['info', 'v2.3 -> v2.4: Yanqing Hair Draw Hash'], ['upgrade_hash', { 'to': '1534d13e' }]],
    'e5457b98': [['info', 'v2.3 -> v2.4: Yanqing Hair Texcoord Hash'], ['upgrade_hash', { 'to': '3ef427fd' }]],
    'e0d7d970': [['info', 'v2.3 -> v2.4: Yanqing Hair Position Hash'], ['upgrade_hash', { 'to': 'a2ee2b45' }]],

    '994d55ab': [['info', 'v2.3 -> v2.4: Yanqing Head Position Hash'], ['upgrade_hash', { 'to': '5bc1537b' }]],
    'ed7ceec2': [['info', 'v2.3 -> v2.4: Yanqing Head Draw Hash'], ['upgrade_hash', { 'to': '04782d92' }]],
    '738ba58f': [['info', 'v2.3 -> v2.4: Yanqing Head Texcoord Hash'], ['upgrade_hash', { 'to': '6d99c7e0' }]],
    '6ae41f8f': [['info', 'v2.3 -> v2.4: Yanqing Head IB Hash'], ['upgrade_hash', { 'to': '9e0449af' }]],

    'a2ee2b45': [['info', 'v2.4 -> v3.1: Yanqing Hair Position Hash'], ['upgrade_hash', { 'to': 'bb5b36f7' }]],
    '5bc1537b': [['info', 'v2.4 -> v3.1: Yanqing Head Position Hash'], ['upgrade_hash', { 'to': '42744ec9' }]],
    '40828c6c': [['info', 'v3.0 -> v3.1: Yanqing Body Blend Hash'], ['upgrade_hash', { 'to': 'af73f238' }]],
    '628a3954': [['info', 'v3.0 -> v3.1: Yanqing Hair Blend Hash'], ['upgrade_hash', { 'to': '8d7b4700' }]],
    '34eb9f8c': [['info', 'v3.0 -> v3.1: Yanqing Head Blend Hash'], ['upgrade_hash', { 'to': 'db1ae1d8' }]],
    '5c21b25d': [['info', 'v3.1 -> v3.2: Yanqing Body Position Hash'], ['upgrade_hash', { 'to': 'b9254412' }]],

    '1534d13e': [['info', 'v2.4 -> v3.2: Yanqing Hair Draw Hash'], ['upgrade_hash', { 'to': '1aa7e433' }]],
    '04782d92': [['info', 'v2.4 -> v3.2: Yanqing Head Draw Hash'], ['upgrade_hash', { 'to': '0beb189f' }]],
    'efd60e3d': [['info', 'v3.1 -> v3.2: Yanqing Body Draw Hash'], ['upgrade_hash', { 'to': 'e0453b30' }]],


    // MARK: Yukong
    '896a066e': [['info', 'v1.4 -> v1.6: Yukong BodyA LightMap Hash'], ['upgrade_hash', { 'to': '052766cf' }]],
    '1d185915': [['info', 'v1.6 -> v2.0: Yukong Body Texcoord Hash'], ['upgrade_hash', { 'to': 'e5e376b8' }]],
    '1df9540b': [
        ['info', 'v1.6 -> v2.0: Yukong Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'YukongBody',
            'hash': '28bbd4ae',
            'trg_indices': ['0', '55551', '60498'],
            'src_indices': ['0', '-1', '60498'],
        }]
    ],

    '08d184a7': [['info', 'v2.2 -> v2.3: Yukong Hair Diffuse Hash'], ['upgrade_hash', { 'to': '6fa27e76' }]],
    '11960703': [['info', 'v2.2 -> v2.3: Yukong Hair LightMap Hash'], ['upgrade_hash', { 'to': '40baf876' }]],

    'b111f58e': [['info', 'v2.2 -> v2.3: Yukong Head Diffuse Hash'], ['upgrade_hash', { 'to': 'bbaa4fba' }]],

    'b6457bdb': [['info', 'v2.2 -> v2.3: Yukong BodyA Diffuse Hash'], ['upgrade_hash', { 'to': '9e0f6958' }]],
    '052766cf': [['info', 'v2.2 -> v2.3: Yukong BodyA LightMap Hash'], ['upgrade_hash', { 'to': '220a5367' }]],

    '0d24ec87': [['info', 'v3.0 -> v3.1: Yukong Body Blend Hash'], ['upgrade_hash', { 'to': 'e2d592d3' }]],
    '3219c61a': [['info', 'v3.0 -> v3.1: Yukong Hair Blend Hash'], ['upgrade_hash', { 'to': 'dde8b84e' }]],
    '96478ccb': [['info', 'v3.0 -> v3.1: Yukong Hair Position Hash'], ['upgrade_hash', { 'to': '8ff29179' }]],
    'e539f4ce': [['info', 'v3.0 -> v3.1: Yukong Head Blend Hash'], ['upgrade_hash', { 'to': '0ac88a9a' }]],
    '1c4be428': [['info', 'v3.0 -> v3.1: Yukong Head Position Hash'], ['upgrade_hash', { 'to': '05fef99a' }]],

    '0ea98fa3': [['info', 'v3.1 -> v3.2: Yukong Hair Draw Hash'], ['upgrade_hash', { 'to': '013abaae' }]],
    'd8a2d73d': [['info', 'v3.1 -> v3.2: Yukong Head Draw Hash'], ['upgrade_hash', { 'to': 'd731e230' }]],
    'eeb20473': [['info', 'v3.1 -> v3.2: Yukong Body Draw Hash'], ['upgrade_hash', { 'to': 'e121317e' }]],
    '5d45b813': [['info', 'v3.1 -> v3.2: Yukong Body Position Hash'], ['upgrade_hash', { 'to': '59dca58e' }]],


    // MARK: Yunli
    // 'afb1f48c': [
    //     ['info', 'v2.4: Yunli Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '8d5695b1'}],
    //     ['multiply_section', {
    //         'titles': ['YunliBodyPosition', 'YunliBodyPosition_Extra'],
    //         'hashes': ['afb1f48c', '8d5695b1']
    //     }]
    // ],

    '8d5695b1': [['info', 'v2.2 -> v3.1: Yunli Body Position Extra Hash'], ['comment_sections', {}]],
    '2ab1d1bd': [['info', 'v3.0 -> v3.1: Yunli Body Blend Hash'], ['upgrade_hash', { 'to': 'c540afe9' }]],
    '22a4c645': [['info', 'v3.0 -> v3.1: Yunli Hair Blend Hash'], ['upgrade_hash', { 'to': 'cd55b811' }]],
    '6c60e749': [['info', 'v3.0 -> v3.1: Yunli Hair Position Hash'], ['upgrade_hash', { 'to': '75d5fafb' }]],
    'be092ccf': [['info', 'v3.0 -> v3.1: Yunli Head Blend Hash'], ['upgrade_hash', { 'to': '51f8529b' }]],
    '44a7e483': [['info', 'v3.0 -> v3.1: Yunli Head Position Hash'], ['upgrade_hash', { 'to': '5d12f931' }]],
    'afb1f48c': [['info', 'v3.1 -> v3.2: Yunli Body Position Hash'], ['upgrade_hash', { 'to': '94e38803' }]],

    '092a10a3': [['info', 'v3.1 -> v3.2: Yunli Hair Draw Hash'], ['upgrade_hash', { 'to': '06b925ae' }]],
    '625e8b72': [['info', 'v3.1 -> v3.2: Yunli Head Draw Hash'], ['upgrade_hash', { 'to': '6dcdbe7f' }]],
    '1c4648ec': [['info', 'v3.1 -> v3.2: Yunli Body Draw Hash'], ['upgrade_hash', { 'to': '13d57de1' }]],


    // MARK: Caelus
    '0bbb3448': [['info', 'v1.5 -> v1.6: Caelus Body Texcoord Hash [Destruction]'], ['upgrade_hash', { 'to': '97c34928' }]],
    '97c34928': [['info', 'v2.7 -> v3.0: Caelus Body Texcoord Hash [Shared]'], ['upgrade_hash', { 'to': '15be9519' }]],
    '44da446d': [['info', 'v1.6: Caelus Body Texcoord Hash [Preservation]'], ['upgrade_else_comment', { 'missing': ['0bbb3448', '97c34928', '15be9519'], 'hash': '0bbb3448' }]],
    '77933d6e': [['info', 'v2.2: Caelus Body Texcoord Hash [Harmony]'], ['upgrade_else_comment', { 'missing': ['0bbb3448', '97c34928', '15be9519'], 'hash': '0bbb3448' }]],

    'f00b031a': [['info', 'v2.7 -> v3.0: Caelus Body Blend Hash'], ['upgrade_hash', { 'to': '2baba62a' }]],
    '9e7ca423': [['info', 'v1.6: Caelus Body Blend Hash [Preservation]'], ['upgrade_else_comment', { 'missing': ['f00b031a', '2baba62a'], 'hash': 'f00b031a' }]],
    '560052ad': [['info', 'v2.2: Caelus Body Blend Hash [Harmony]'], ['upgrade_else_comment', { 'missing': ['f00b031a', '2baba62a'], 'hash': 'f00b031a' }]],

    '91022b8f': [['info', 'v2.7 -> v3.0: Caelus Body Draw Hash'], ['upgrade_hash', { 'to': '33342be6' }]],
    '22f597ef': [['info', 'v2.7 -> v3.0: Caelus Body Position Hash'], ['upgrade_hash', { 'to': '80c39786' }]],

    '80c39786': [['info', 'v3.0 -> v3.1: Caelus Body Position Hash'], ['upgrade_hash', { 'to': 'aa2648d4' }]],
    // '80c39786': [
    //     ['info', 'v3.0: Caelus Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': 'b3935566'}],
    //     ['multiply_section', {
    //         'titles': ['CaelusBodyPosition', 'CaelusBodyPosition_Extra'],
    //         'hashes': ['80c39786', 'b3935566']
    //     }]
    // ],
    'b3935566': [['info', 'v3.0 -> v3.1: Caelus Body Position Hash'], ['comment_sections', {}]],


    'fd65164c': [
        ['info', 'v1.5 -> v1.6: Caelus Body IB Hash [Destruction]'],
        ['multiply_indexed_section', {
            'title': 'CaelusBody',
            'hash': 'e3ffef9a',
            'trg_indices': ['0', '38178'],
            'src_indices': ['-1', '0'],
        }]
    ],
    'e3ffef9a': [
        ['info', 'v2.7 -> v3.0: Caelus Body IB Hash'],
        ['multiply_indexed_section', {
            'title': 'CaelusBody',
            'hash': '825f79f5',
            'trg_indices': ['0', '37659'],
            'src_indices': ['0', '38178'],
        }]
    ],
    'a270e292': [
        ['info', 'v1.6: Caelus Body IB Hash [Preservation]'],
        ['upgrade_else_comment_indexed', {
            'missing': ['fd65164c', 'e3ffef9a', '825f79f5'],
            'title': 'CaelusBody',
            'hash': '825f79f5',
            'trg_indices': ['0', '37659'],
            'src_indices': ['0', '37674'],
        }]
    ],
    '89fcb592': [
        ['info', 'v2.2: Caelus Body IB Hash [Harmony]'],
        ['upgrade_else_comment_indexed', {
            'missing': ['fd65164c', 'e3ffef9a', '825f79f5'],
            'title': 'CaelusBody',
            'hash': '825f79f5',
            'trg_indices': ['0', '37659'],
            'src_indices': ['0', '39330'],
        }]
    ],


    '3fc38f8a': [['info', 'v2.2 -> v2.3: Caelus Hair Texcoord Hash'], ['upgrade_hash', { 'to': 'f4f5c11d' }]],
    '7de7f0c0': [['info', 'v2.2 -> v2.3: Caelus Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'fa0975b2' }]],
    'c17e8830': [['info', 'v2.2 -> v2.3: Caelus Hair LightMap Hash'], ['upgrade_hash', { 'to': 'd75c3881' }]],

    'd667a346': [['info', 'v2.7 -> v3.0: Caelus Head Position Hash'], ['upgrade_hash', { 'to': '87f2f3ce' }]],
    '7409246c': [['info', 'v2.3: Caelus Head Position Hash [Harmony]'], ['upgrade_else_comment', { 'missing': ['d667a346', '87f2f3ce'], 'hash': '87f2f3ce' }]],

    '5df1352e': [['info', 'v2.7 -> v3.0: Caelus Head Draw Hash'], ['upgrade_hash', { 'to': '1c71430d' }]],
    'abdc67e6': [['info', 'v2.7 -> v3.0: Caelus Head Blend Hash'], ['upgrade_hash', { 'to': '3576ec0a' }]],
    '9108c1f1': [['info', 'v2.7 -> v3.0: Caelus Head Texcoord Hash'], ['upgrade_hash', { 'to': '714d71d0' }]],
    'c1004613': [
        ['info', 'v2.7 -> v3.0: Caelus Head IB Hash'],
        ['multiply_indexed_section', {
            'title': 'CaelusHead',
            'hash': '70f89eb8',
            'trg_indices': ['0', '13734'],
            'src_indices': ['0', '13716'],
        }]
    ],


    'b193e6d8': [['info', 'v2.2 -> v2.3: Caelus Head Diffuse Hash'], ['upgrade_hash', { 'to': '21b96557' }]],

    '28d09106': [['info', 'v2.2 -> v2.3: Caelus Body Diffuse Hash'], ['upgrade_hash', { 'to': '3e8e34d5' }]],
    '0fe66c92': [['info', 'v2.2 -> v2.3: Caelus Body LightMap Hash'], ['upgrade_hash', { 'to': '6194fa1b' }]],

    '2baba62a': [['info', 'v3.0 -> v3.1: Caelus Body Blend Hash'], ['upgrade_hash', { 'to': 'c45ad87e' }]],
    '87f2f3ce': [['info', 'v3.0 -> v3.1: Caelus Head Position Hash'], ['upgrade_hash', { 'to': '9e47ee7c' }]],
    '6786de68': [['info', 'v3.0 -> v3.1: CaelusDestruction Hair Blend Hash'], ['upgrade_hash', { 'to': '8877a03c' }]],
    '870564f1': [['info', 'v3.0 -> v3.1: CaelusDestruction Hair Position Hash'], ['upgrade_hash', { 'to': '9eb07943' }]],

    '3437de1d': [['info', 'v3.1 -> v3.2: CaelusRemembrance Hair Draw Hash'], ['upgrade_hash', { 'to': '3ba4eb10' }]],
    '1c71430d': [['info', 'v3.1 -> v3.2: CaelusRemembrance Head Draw Hash'], ['upgrade_hash', { 'to': '13e27600' }]],
    '33342be6': [['info', 'v3.1 -> v3.2: CaelusRemembrance Body Draw Hash'], ['upgrade_hash', { 'to': '3ca71eeb' }]],


    // MARK: Stelle
    //     Skip adding extra sections for v1.6, v2.0, v2.1 Preservation hashes
    //     because those extra sections are not needed in v2.2
    //     Comment out the extra sections later
    '01df48a6': [['info', 'v1.5 -> v1.6: Body Texcoord Hash [Stelle]'], ['upgrade_hash', { 'to': 'a68ffeb1' }]],
    'a68ffeb1': [
        ['info', 'v2.1 -> v2.2: Body Texcoord Hash [Destruction Stelle]'],
        ['upgrade_hash', { 'to': 'f00b6ded' }]
    ],

    '85ad43b3': [
        ['info', 'v1.5 -> v1.6: Body IB Hash [Stelle]'],
        ['multiply_indexed_section', {
            'title': 'StelleBody',
            'hash': '174a08d4',
            'trg_indices': ['0', '32967'],
            'src_indices': ['-1', '0'],
        }]
    ],
    '174a08d4': [
        ['info', 'v2.1 -> v2.2: Body IB Hash [Destruction Stelle]'],
        ['multiply_indexed_section', {
            'title': 'StelleBody',
            'hash': 'fba309df',
            'trg_indices': ['0', '32946'],
            'src_indices': ['0', '32967'],
        }]
    ],

    '1a415a73': [['info', 'v2.1 -> v2.2: Stelle Hair Draw Hash'], ['upgrade_hash', { 'to': '00d0c31d' }]],
    '938b9c8f': [['info', 'v2.1 -> v2.2: Stelle Hair Position Hash'], ['upgrade_hash', { 'to': '8c0c078f' }]],
    '8680469b': [['info', 'v2.1 -> v2.2: Stelle Hair Texcoord Hash'], ['upgrade_hash', { 'to': 'fe9eaef0' }]],
    '2d9adf2d': [['info', 'v2.1 -> v2.2: Stelle Hair IB Hash'], ['upgrade_hash', { 'to': '1d62eafb' }]],

    'fdb54553': [['info', 'v2.2 -> v2.3: Stelle Hair Diffuse Hash'], ['upgrade_hash', { 'to': 'a04fcf6f' }]],
    'ef5586c1': [['info', 'v2.2 -> v2.3: Stelle Hair LightMap Hash'], ['upgrade_hash', { 'to': '02a9b085' }]],

    '1c0a8ff8': [['info', 'v2.2 -> v2.3: Stelle Head Diffuse Hash'], ['upgrade_hash', { 'to': '4e98df53' }]],

    'a19a8d2c': [['info', 'v2.2 -> v2.3: Stelle Body Diffuse Hash'], ['upgrade_hash', { 'to': '78d10c03' }]],
    '5d15eefe': [['info', 'v2.2 -> v2.3: Stelle Body LightMap Hash'], ['upgrade_hash', { 'to': '69014337' }]],

    // '6949f854': [
    //     ['info', 'v3.0: Stelle Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': 'e9e81b6c'}],
    //     ['multiply_section', {
    //         'titles': ['StelleBodyPosition', 'StelleBodyPosition_Extra'],
    //         'hashes': ['6949f854', 'e9e81b6c']
    //     }]
    // ],

    'e9e81b6c': [['info', 'v2.2 -> v3.1: Stelle Body Position Extra Hash'], ['comment_sections', {}]],

    // Comment out the sections with hashes no longer used in v2.2
    '2dcd5dc0': [['info', 'v2.1: Comment Body Texcoord Hash [Preservation Stelle]'], ['comment_sections', {}]],
    'e0d86dc8': [['info', 'v2.1: Comment Body IB Hash [Preservation Stelle]'], ['comment_sections', {}]],

    '8c0c078f': [['info', 'v2.2 -> v3.1: Stelle Hair Position Hash'], ['upgrade_hash', { 'to': '95b91a3d' }]],
    '5aadfa65': [['info', 'v3.0 -> v3.1: Stelle Body Blend Hash'], ['upgrade_hash', { 'to': 'b55c8431' }]],
    '46ed784a': [['info', 'v3.0 -> v3.1: Stelle Hair Blend Hash'], ['upgrade_hash', { 'to': 'a91c061e' }]],
    '45a18e05': [['info', 'v3.0 -> v3.1: Stelle Head Blend Hash'], ['upgrade_hash', { 'to': 'aa50f051' }]],
    '00658faa': [['info', 'v3.0 -> v3.1: Stelle Head Position Hash'], ['upgrade_hash', { 'to': '19d09218' }]],


    '00d0c31d': [['info', 'v2.2 -> v3.2: Stelle Hair Draw Hash'], ['upgrade_hash', { 'to': '0f43f610' }]],
    '9368f26c': [['info', 'v3.1 -> v3.2: Stelle Head Draw Hash'], ['upgrade_hash', { 'to': '9cfbc761' }]],
    'dabe4434': [['info', 'v3.1 -> v3.2: Stelle Body Draw Hash'], ['upgrade_hash', { 'to': 'd52d7139' }]],
    '6949f854': [['info', 'v3.1 -> v3.2: Stelle Body Position Hash'], ['upgrade_hash', { 'to': 'f05d06de' }]],




    // MARK: Other Entity Fixes





    // MARK: Svarog
    'ae587fb2': [['info', 'v2.2 -> v2.3: Svarog BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'ae37a552' }]],
    'a3acad6f': [['info', 'v2.2 -> v2.3: Svarog BodyA LightMap Hash'], ['upgrade_hash', { 'to': 'd653a999' }]],
    '10beb640': [['info', 'v2.2 -> v2.3: Svarog BodyA Diffuse Hash'], ['upgrade_hash', { 'to': 'e3a7f3fd' }]],
    '69840f72': [['info', 'v2.2 -> v2.3: Svarog BodyA LightMap Hash'], ['upgrade_hash', { 'to': '4090cc01' }]],



    // MARK: Numby
    '85d1b3ce': [['info', 'v2.2 -> v2.3: Numby Body DiffuseChScreen Hash'], ['upgrade_hash', { 'to': 'e22b4c5e' }]],
    'dab1477d': [['info', 'v2.2 -> v2.3: Numby Body DiffuseOverworldCombat Hash'], ['upgrade_hash', { 'to': '6cad0819' }]],
    'a313ad5f': [['info', 'v2.2 -> v2.3: Numby Body LightMapChScreen Hash'], ['upgrade_hash', { 'to': '07471bf5' }]],
    '807fb688': [['info', 'v2.2 -> v2.3: Numby Body LightMapOverworld Hash'], ['upgrade_hash', { 'to': '02644fcc' }]],
    'ef40ac05': [['info', 'v2.2 -> v2.3: Numby Body LightMapCombat Hash'], ['upgrade_hash', { 'to': 'cd7acd1a' }]],

    // '9afaa7d9': [
    //     ['info', 'v2.1: Numby Body Position: Apply Vertex Explosion Fix'],
    //     ['check_hash_not_in_ini', {'hash': '394111ad'}],
    //     ['multiply_section', {
    //         'titles': ['NumbyBodyPosition', 'NumbyBodyPosition_Extra'],
    //         'hashes': ['9afaa7d9', '394111ad']
    //     }]
    // ],
    '394111ad': [['info', 'v2.2 -> v3.1: Numby Body Position Extra Hash'], ['comment_sections', {}]],


    // MARK: Weapons


    '7ae27f17': [['info', 'v2.2 -> v2.3: Jingliu Sword Diffuse Hash'], ['upgrade_hash', { 'to': 'b71e3abe' }]],
    '6acc5dd1': [['info', 'v2.2 -> v2.3: Jingliu Sword LightMap Hash'], ['upgrade_hash', { 'to': '12fde9bd' }]],


    '52e8727a': [['info', 'v2.2 -> v2.3: March7th Bow Diffuse Hash'], ['upgrade_hash', { 'to': '91804076' }]],
    'f47e4ed8': [['info', 'v2.2 -> v2.3: March7th Bow LightMap Hash'], ['upgrade_hash', { 'to': 'e91ab48f' }]],


    'c69a4a5f': [['info', 'v2.2 -> v2.3: Trailblazer Bat Diffuse Hash'], ['upgrade_hash', { 'to': 'cac102b9' }]],
    'bc86078f': [['info', 'v2.2 -> v2.3: Trailblazer Bat Diffuse Ult Hash'], ['upgrade_hash', { 'to': '4a638b94' }]],
    'c7969478': [['info', 'v2.2 -> v2.3: Trailblazer Bat LightMap Hash'], ['upgrade_hash', { 'to': 'ff6df1ec' }]],

    '0a27a48e': [['info', 'v2.2 -> v2.3: Trailblazer Spear Diffuse Hash'], ['upgrade_hash', { 'to': '4cd9ab1d' }]],
    '7ce10d72': [['info', 'v2.2 -> v2.3: Trailblazer Spear LightMap Hash'], ['upgrade_hash', { 'to': 'bdae2ad0' }]],

    '685495d0': [['info', 'v2.2 -> v2.3: Seele Scythe Diffuse Hash'], ['upgrade_hash', { 'to': 'ce802067' }]],
    '910e8419': [['info', 'v2.2 -> v2.3: Seele Scythe LightMap Hash'], ['upgrade_hash', { 'to': 'cb875574' }]],


    // MARK: Unknown meshes that were added to Sora's 3.0 -> 3.1 hash update fix
    '253b0ea5': [['info', 'v3.0 -> v3.1: SilvermaneSoldier Body Blend Hash'], ['upgrade_hash', { 'to': 'caca70f1' }]],
    '9e4fb633': [['info', 'v3.0 -> v3.1: SilvermaneSoldier Body Position Hash'], ['upgrade_hash', { 'to': '87faab81' }]],
    '41b400fe': [['info', 'v3.0 -> v3.1: Siobhan Body Blend Hash'], ['upgrade_hash', { 'to': 'ae457eaa' }]],
    'e7b4d744': [['info', 'v3.0 -> v3.1: Siobhan Body Position Hash'], ['upgrade_hash', { 'to': 'fe01caf6' }]],
    '0f0feeb5': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e0fe90e1' }]],
    '7c81efa9': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '6534f21b' }]],
    'f79bf55c': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'ee2ee8ee' }]],
    '88964833': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '67673667' }]],
    'de1818d4': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c7ad0566' }]],
    'c3912b58': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c7ad0566' }]],
    '8124d93b': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '6ed5a76f' }]],
    // 'e2fb7ce0': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'],    ['upgrade_hash', {'to': 'fb4e6152'}]], // bailu extra hash 3.0 to 3.2
    // '10fb3cab': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'],    ['upgrade_hash', {'to': '094e2119'}]], // blackswan extra hash 3.0 to 3.2
    '90a94c09': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '4d61fb3a' }]],
    'aa594182': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '45a83fd6' }]],
    'fb3eae6f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e28bb3dd' }]],
    '6b52f38e': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '776a0dad' }]],
    '40c9c4e2': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e2ce4067' }]],
    'f1e7fb1f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e852e6ad' }]],
    '081b6cb4': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e7ea12e0' }]],
    // '85d02e23': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'],    ['upgrade_hash', {'to': '9c653391'}]], // feixiao pos hash 3.0 to 3.2
    '99c3e07e': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '8076fdcc' }]],
    '9525638d': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '8c907e3f' }]],
    '7d17af56': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '64a2b2e4' }]],
    '869bd4b2': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '9f2ec900' }]],
    '9963bd2c': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '80d6a09e' }]],
    'e0a23cff': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'f917214d' }]],
    // '04d0f9a0': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'],    ['upgrade_hash', {'to': 'eb2187f4'}]],
    '11a90c69': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '081c11db' }]],
    '7fb40d9c': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3c4256b0' }]],
    '3fe3d055': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'd012ae01' }]],

    '472e6131': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'a8df1f65' }]],
    'caf79f9b': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '2506e1cf' }]],
    'aa0733d3': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'b3b22e61' }]],
    '403a12dd': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'afcb6c89' }]],
    'c0a7cb23': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '5eab2525' }]],
    '84eb69d9': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '6b307cf7' }]],
    '7243533d': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '9db22d69' }]],
    'f795f7c5': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'ee20ea77' }]],
    '425deed9': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '01abb5f5' }]],
    // '7e4f7890': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'],    ['upgrade_hash', {'to': '67fa6522'}]], // RUAN MEIN POS hash 3.0 to 3.2
    'b1be2710': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'a80b3aa2' }]],
    '0385188d': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '407343a1' }]],

    '54d8694d': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'bb291719' }]],
    '79daebcd': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '606ff67f' }]],
    '9be881fe': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '7419ffaa' }]],
    'cdc9515a': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '22382f0e' }]],
    '1663ad11': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'f992d345' }]],
    'fe4495a6': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e7f18814' }]],
    '7dd3bc06': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '9222c252' }]],
    'caf6b434': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '2507ca60' }]],
    'dda35578': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c41648ca' }]],
    'e9d817e8': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '062969bc' }]],
    '8287eb6e': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '9b32f6dc' }]],
    'f8152c40': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '17e45214' }]],
    '5e7a6aad': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '47cf771f' }]],
    'bda9c73f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '5258b96b' }]],
    'af18050f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'b6ad18bd' }]],
    'abd8978e': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '4429e9da' }]],
    'c487e3ef': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'dd32fe5d' }]],
    '7bd8bb12': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '9429c546' }]],
    '667bbd6f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '7fcea0dd' }]],
    'e553e2c7': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '0aa29c93' }]],
    '2af3a2df': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'd039e386' }]],
    'd95a13b5': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '5b958520' }]],
    '3a57ca1e': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'b0f43db2' }]],
    '05bd6e65': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '473e01f2' }]],
    'f7836426': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '7b955957' }]],
    '38186b93': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3af9021d' }]],
    '527c39d7': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'f1751019' }]],
    'f942391e': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '7da1cd27' }]],
    '3d12735a': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '94b37144' }]],
    'da48c709': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '44e14830' }]],
    '6205432d': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3af9021d' }]],
    '27bf8ebf': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'f1751019' }]],
    '7abd2d8f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '7da1cd27' }]],
    'b10b2e91': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '94b37144' }]],
    'a23ddb70': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '44e14830' }]],
    '4054a3fe': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '7549a8b8' }]],
    '960e77ad': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '2f74813a' }]],
    '2c263e6c': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'ed4c8417' }]],
    '46f3ff5f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '7f862c3d' }]],
    '938a531f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '7f862c3d' }]],
    'b2206637': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '2c760e0b' }]],
    'c71aa0e9': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'deafbd5b' }]],
    '0d459ac9': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e2b4e49d' }]],
    'e752099b': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'fee71429' }]],
    '3747e5ed': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'd8b69bb9' }]],
    'cdd6cd01': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'd463d0b3' }]],
    '4846e3fa': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'a7b79dae' }]],
    'b263bc44': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'abd6a1f6' }]],
    '34e69a64': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'db17e430' }]],
    'd2d88f62': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'cb6d92d0' }]],
    'f1f30cf3': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '1e0272a7' }]],
    '7a26f186': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '6393ec34' }]],
    'a55c5aaf': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '4aad24fb' }]],
    'b1f10cd5': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'a8441167' }]],
    '6de96e6e': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '8218103a' }]],
    '8898dbb9': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '912dc60b' }]],
    '512b60d1': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'beda1e85' }]],
    '3f55afb2': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '26e0b200' }]],
    '75bec472': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '9a4fba26' }]],
    '1f5654c4': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '06e34976' }]],
    'f61e2627': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '19ef5873' }]],
    '93b253e7': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '8a074e55' }]],
    '157d3961': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'fa8c4735' }]],
    'ef07afd2': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'f6b2b260' }]],
    'b06e7593': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '5f9f0bc7' }]],
    '80429718': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '99f78aaa' }]],
    '89743b50': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '66854504' }]],
    '49f9d1e5': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '504ccc57' }]],
    '9cce1165': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '733f6f31' }]],
    'e94f5824': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'f0fa4596' }]],
    'a3fdf5fe': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e00baed2' }]],
    '4f07ac05': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '56b2b1b7' }]],
    'a46c436b': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e79a1847' }]],
    '759fa196': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '6c2abc24' }]],
    '845a071f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c7ac5c33' }]],
    '07814a7e': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '1e3457cc' }]],
    'ef66faaa': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'ac90a186' }]],
    'cc00c3a7': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'd5b5de15' }]],
    'a0f13cc1': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e30767ed' }]],
    '2e051c5d': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '37b001ef' }]],
    '9a134c5c': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'd9e51770' }]],
    'f0880cff': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e93d114d' }]],
    'b15f1f0a': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'f2a94426' }]],
    '28365225': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '31834f97' }]],
    '4ccac05b': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '0f3c9b77' }]],
    'f9f9c6d6': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e04cdb64' }]],
    '1641072e': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '55b75c02' }]],
    '0e471e54': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '17f203e6' }]],
    '2fbe1332': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '6c48481e' }]],
    '484b5014': [['info', 'v3.0 -> v3.1: Garment Maker Body Position Hash'], ['upgrade_hash', { 'to': '51fe4da6' }]],
    '31d815ee': [['info', 'v3.0 -> v3.1: Garment Maker Body Blend Hash'], ['upgrade_hash', { 'to': 'de296bba' }]],
    'df444c4c': [['info', 'v3.0 -> v3.1: Garment Maker Sword Position Hash'], ['upgrade_hash', { 'to': 'c6f151fe' }]],
    '13483937': [['info', 'v3.0 -> v3.1: Garment Maker Sword Blend Hash'], ['upgrade_hash', { 'to': 'fcb94763' }]],
    '4e8967f8': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '573c7a4a' }]],
    '8f549559': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '60a5eb0d' }]],
    '181dc042': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '01a8ddf0' }]],
    '24a44dd9': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'cb55338d' }]],
    'e5328f4c': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'fc8792fe' }]],
    '6e1dccfd': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '81ecb2a9' }]],
    '96f5d1cf': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '8f40cc7d' }]],
    'a080f5ba': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '4f718bee' }]],
    '6a5b23b0': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '73ee3e02' }]],
    'f3147d2f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '1ce5037b' }]],
    '796716a5': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '60d20b17' }]],
    'c5334397': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '2ac23dc3' }]],
    'ba600701': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'a3d51ab3' }]],
    '303e84b6': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'dfcffae2' }]],
    'fb858acf': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e230977d' }]],
    'd7a04dcf': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3851339b' }]],
    '22b0ae41': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3b05b3f3' }]],
    '9b16dd34': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '74e7a360' }]],
    '84e7c2ad': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '9d52df1f' }]],
    'a186a3f3': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '4e77dda7' }]],
    '3cc75b26': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '25724694' }]],
    '41ffe904': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'ae0e9750' }]],
    '5b76a8e4': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '42c3b556' }]],
    '78e0e741': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '97119915' }]],
    'dff98b92': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c64c9620' }]],
    'c7d28178': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '2823ff2c' }]],
    '4ec4ff4a': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '5771e2f8' }]],
    '469555d4': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'a9642b80' }]],
    '839fb43d': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '9a2aa98f' }]],
    '03f7cc0c': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'ec06b258' }]],
    '3a2a38ec': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '239f255e' }]],
    '0dc8b8b1': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e239c6e5' }]],
    '17b8acfd': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '0e0db14f' }]],
    'dee2cb8f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3113b5db' }]],
    'cb0e6b81': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'd2bb7633' }]],
    'f61407ea': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '19e579be' }]],
    'f391ee20': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '40737fdd' }]],
    '267ee4a0': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3fcbf912' }]],
    '2bef9f4d': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c41ee119' }]],
    '051dec78': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '1ca8f1ca' }]],
    '18b574c0': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'f7440a94' }]],
    '5685e826': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '4f30f594' }]],
    '4b791087': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'a4886ed3' }]],
    '9e2ed0a2': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '879bcd10' }]],
    '00f5d9a1': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'ef04a7f5' }]],
    '78d8b9e7': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '616da455' }]],
    '05a7b795': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'ea56c9c1' }]],
    '0fe45c8c': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '1651413e' }]],
    '731e330f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '9cef4d5b' }]],
    '639b96a6': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '7a2e8b14' }]],
    '4a097720': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'a5f80974' }]],
    '3fc8d61e': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '267dcbac' }]],
    '0aef4192': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e51e3fc6' }]],
    '0f37b1b2': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '1682ac00' }]],
    'b27ef4d9': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '5d8f8a8d' }]],
    '2e3cadac': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3789b01e' }]],
    'ee06a355': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '01f7dd01' }]],
    '08f85552': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '114d48e0' }]],
    '08600875': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e7917621' }]],
    'dfac1dae': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c619001c' }]],
    '38870c9d': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'd77672c9' }]],
    'ae3604aa': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'b7831918' }]],
    '91122253': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '7ee35c07' }]],
    '99c4cd87': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '8071d035' }]],
    'd58cd44b': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3a7daa1f' }]],
    '23c4f7e1': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3a71ea53' }]],
    '3bb80caf': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'd44972fb' }]],
    'fa6f2140': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e3da3cf2' }]],
    'a788062f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '4879787b' }]],
    '888b8d61': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '913e90d3' }]],
    '35d73a8d': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'da2644d9' }]],
    'f1a27cb3': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e8176101' }]],
    '252d24d2': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'cadc5a86' }]],
    '56313ed8': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '4f84236a' }]],
    '71504f62': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '9ea13136' }]],
    'bdd0dcec': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'a465c15e' }]],
    'eeb11ab9': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'f704070b' }]],
    'a0da18b7': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '4f2b66e3' }]],
    'f5066a2b': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'ecb37799' }]],
    '661aa71e': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '89ebd94a' }]],
    '7e427e16': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '67f763a4' }]],
    'eac4ead3': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '05359487' }]],
    '8db73e2a': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '94022398' }]],
    '258b5a81': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'ca7a24d5' }]],
    '6c61253f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '75d4388d' }]],
    'b9c70ad2': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '56367486' }]],
    '588d2d50': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '413830e2' }]],
    '0ac48257': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e535fc03' }]],
    '4c350a3d': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '5580178f' }]],
    'b6d18831': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '5920f665' }]],
    'd87f36f5': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c1ca2b47' }]],
    '1a4e3624': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'f5bf4870' }]],
    'df0fd15c': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c6baccee' }]],
    'd9f69a39': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3607e46d' }]],
    '77505e42': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '6ee543f0' }]],
    '2614a98d': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c9e5d7d9' }]],
    '4362ba7e': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '5ad7a7cc' }]],
    'e30281de': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '0cf3ff8a' }]],
    'b62379c1': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'af966473' }]],
    '5c1fdf60': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'b3eea134' }]],
    '373d10bf': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '2e880d0d' }]],
    'f6fe3359': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '190f4d0d' }]],
    '8b209de2': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '92958050' }]],
    'b467336d': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '5b964d39' }]],
    '8cd12bf9': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '9564364b' }]],
    '61a07507': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '8e510b53' }]],
    '39ca48a0': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '207f5512' }]],
    '88a29256': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '6753ec02' }]],
    'da18ef05': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c3adf2b7' }]],
    '323c8898': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'ddcdf6cc' }]],
    'bb844ba7': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e1446be0' }]],
    '91aec744': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '334192b1' }]],
    'f7e9b490': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '6a4133f4' }]],
    '0a345e81': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'a55fda83' }]],
    '74ed7efc': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '15313587' }]],
    'e1c07b3c': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c0f69713' }]],
    '812273c7': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '98b4fa51' }]],
    '4e17629f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '8493de99' }]],
    '3ac64e77': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '6ad3a6f8' }]],
    'f2362641': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'ecaf7432' }]],
    '00298224': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '6916750a' }]],
    '2b8e11df': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '323b0c6d' }]],
    '837da7fe': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c08bfcd2' }]],
    'd3f9d51f': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'ca4cc8ad' }]],
    'c365ae0b': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '2c94d05f' }]],
    'fb82ffbc': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'e237e20e' }]],
    'd2d4f52a': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3d258b7e' }]],
    'e7ed8a5a': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'fe5897e8' }]],
    '88273782': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '67d649d6' }]],
    '07c849d7': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '1e7d5465' }]],
    '8b0fbf16': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '64fec142' }]],
    'd4d3a4aa': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'cd66b918' }]],
    'd7e8dab8': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3819a4ec' }]],
    'db22c75e': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'c297daec' }]],
    '6c9875d4': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '752d6866' }]],
    '58637f0b': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'b792015f' }]],
    '7c5713d5': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '65e20e67' }]],
    'ff9789b2': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '1066f7e6' }]],
    '606fbb39': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '79daa68b' }]],
    '40393e28': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'afc8407c' }]],
    '9863bd15': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '81d6a0a7' }]],
    'd21fd2f9': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '3deeacad' }]],
    'c7827198': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'de376c2a' }]],
    '2dbdd789': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '163ba24a' }]],
    'ba5cb237': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '1a6fe208' }]],
    '8ccd033a': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '9449348b' }]],
    '782a2923': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': 'a5b56e16' }]],
    'ceb80f3b': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '71296430' }]],
    '26e02ee4': [['info', 'v3.0 -> v3.1: UNKNOWN UNKNOWN Unknown Hash'], ['upgrade_hash', { 'to': '19759134' }]],
};