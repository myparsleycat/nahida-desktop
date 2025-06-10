import fse from 'fs-extra';
import path from 'node:path';

export const version = '3.3';

interface SectionData {
    name: string;
    hash: string;
    lines: string[];
}

class IniProcessor {
    private static readonly VALID_HASH_TRIOS: Record<string, [string, string, string]> = {
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
        "HyacineBody": ["4535e8b6", "42915465", "bae12d5a"],
        "HyacineHair": ["c1cdf8cf", "c7ab6927", "ae3b4d8a"],
        "HyacineFace": ["8c18617d", "51e9543e", "5417235e"],
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

    private filepath: string;
    private vertcountByHash: Record<string, string> = {};
    private resultLines: string[] = [];
    private currentSection: SectionData = { name: '', hash: '', lines: [] };
    private allSections: SectionData[] = [];
    private mergedModDetected: boolean = false;
    private highestVertcount: number = 0;
    private vertexLimitMax: number = 0;
    private modified: boolean = false;

    constructor(filepath: string) {
        this.filepath = filepath;
    }

    private static backupPath(filePath: string): string {
        const folder = path.dirname(filePath);
        const base = path.basename(filePath);
        return path.join(folder, `DISABLED_${base}`);
    }

    private detectMergedMod(): void {
        for (const section of this.allSections) {
            if (section.name.toLowerCase().startsWith('commandlist')) {
                let blockDepth = 0;
                for (const line of section.lines) {
                    const stripped = line.trim().toLowerCase();
                    if (stripped.startsWith('if')) {
                        blockDepth += 1;
                    } else if (stripped.startsWith('endif')) {
                        blockDepth -= 1;
                    } else if (stripped.startsWith('else') || stripped.startsWith('elif') || stripped.startsWith('elseif')) {
                        this.mergedModDetected = true;
                        console.log('[INFO] Merged mod detected: True');
                        return;
                    }
                }
            }
        }
        console.log('[INFO] Merged mod detected: False');
    }

    private getVertcountHashFor(sectionHash: string): string | null {
        const sh = sectionHash.toLowerCase();
        for (const trio of Object.values(IniProcessor.VALID_HASH_TRIOS)) {
            if (sh === trio[0] || sh === trio[1]) {
                for (const t of [trio[0], trio[1]]) {
                    if (t in this.vertcountByHash) {
                        return t;
                    }
                }
            }
        }
        return null;
    }

    private static cleanNewPipelineSection(lines: string[]): string[] {
        const cleaned: string[] = [];
        let insideDraw8 = false;
        let blockStarted = false;

        for (const line of lines) {
            const modifiedLine = line.replace('$\\SRMI\\vertcount', '$\\SRMI\\vertex_count');
            const stripped = modifiedLine.trim();

            if (!insideDraw8 && /^Resource.*DrawCS\s*=/.test(stripped)) {
                continue;
            }

            if (insideDraw8 && stripped.startsWith('Resource\\SRMI\\DrawBuffer')) {
                continue;
            }

            if (/^\$_blend_\s*=/.test(stripped)) {
                continue;
            }

            if (stripped.startsWith('if ')) {
                blockStarted = true;
            }

            if (!blockStarted) {
                cleaned.push(modifiedLine);
                continue;
            }

            if (stripped.startsWith('if DRAW_TYPE == 8')) {
                insideDraw8 = true;
                cleaned.push(modifiedLine);
                continue;
            }

            if (insideDraw8) {
                if (
                    stripped.startsWith('Resource\\SRMI\\PositionBuffer') ||
                    stripped.startsWith('Resource\\SRMI\\BlendBuffer') ||
                    stripped.startsWith('$\\SRMI\\vertex_count') ||
                    stripped === 'endif'
                ) {
                    cleaned.push(modifiedLine);
                }
                if (stripped === 'endif') {
                    cleaned.push('\n');
                    insideDraw8 = false;
                }
                continue;
            }

            cleaned.push(modifiedLine);
        }
        return cleaned;
    }

    private flushSection(): void {
        const sec = this.currentSection;
        let modified = false;
        let newSection: string[] = [];

        const isDrawHash = Object.values(IniProcessor.VALID_HASH_TRIOS).some(
            trio => sec.hash && sec.hash.toLowerCase() === trio[1]
        );
        const isBlendHash = Object.values(IniProcessor.VALID_HASH_TRIOS).some(
            trio => sec.hash && sec.hash.toLowerCase() === trio[0]
        );

        if (sec.name && sec.hash && isBlendHash && this.mergedModDetected) {
            console.log(`[INFO] Cleaning BLEND section for merged mod: ${sec.name} (hash: ${sec.hash})`);
            modified = true;
            newSection.push(`[${sec.name}]\n`);
            let hashLineSeen = false;
            let insideDraw8 = false;

            for (let i = 1; i < sec.lines.length; i++) {
                const line = sec.lines[i].replace('$\\SRMI\\vertcount', '$\\SRMI\\vertex_count');
                const stripped = line.trim();

                if (stripped.toLowerCase().startsWith('hash =')) {
                    if (!hashLineSeen) {
                        newSection.push(line);
                        hashLineSeen = true;
                    }
                    continue;
                }

                if (/^Resource.*DrawCS\s*=\s*copy\s+Resource.*DrawCS/i.test(stripped)) {
                    continue;
                }

                if (stripped.toLowerCase().startsWith('if draw_type == 8')) {
                    insideDraw8 = true;
                    newSection.push(line);
                    continue;
                }

                if (insideDraw8) {
                    if (stripped.toLowerCase() === 'endif') {
                        insideDraw8 = false;
                        newSection.push(line);
                        newSection.push('\n');
                        continue;
                    }

                    if (
                        stripped.startsWith('Resource\\SRMI\\PositionBuffer') ||
                        stripped.startsWith('Resource\\SRMI\\BlendBuffer') ||
                        stripped.includes('$\\SRMI\\vertex_count')
                    ) {
                        newSection.push(line);
                    }
                    continue;
                }

                newSection.push(line);
            }
        } else if (sec.name && isDrawHash) {
            const vertexCount = this.mergedModDetected
                ? String(Math.max(this.highestVertcount, this.vertexLimitMax))
                : this.vertcountByHash[this.getVertcountHashFor(sec.hash) || ''] || '0';

            modified = true;
            newSection = [
                `[${sec.name}]\n`,
                `hash = ${sec.hash}\n`,
                `override_vertex_count = ${vertexCount}\n`,
                'override_byte_stride = 40\n',
                'uav_byte_stride = 4\n',
                '\n'
            ];
        } else if (sec.name && sec.hash && sec.hash.toLowerCase() in this.vertcountByHash) {
            modified = true;
            newSection.push(`[${sec.name}]\n`);
            let hashLineSeen = false;

            for (let i = 1; i < sec.lines.length; i++) {
                const line = sec.lines[i].replace('$\\SRMI\\vertcount', '$\\SRMI\\vertex_count');
                if (line.trim().toLowerCase().startsWith('hash =')) {
                    if (!hashLineSeen) {
                        newSection.push(line);
                        hashLineSeen = true;
                    }
                } else {
                    newSection.push(line);
                }
            }

            const filteredLines = IniProcessor.cleanNewPipelineSection(newSection.slice(1));
            newSection = [newSection[0], ...filteredLines];
        } else {
            const replaced = sec.lines.map(line => line.replace('$\\SRMI\\vertcount', '$\\SRMI\\vertex_count'));
            this.resultLines.push(...replaced);
            this.currentSection = { name: '', hash: '', lines: [] };
            return;
        }

        if (modified) {
            console.log(`[INFO] Section modified: ${sec.name}`);
            this.modified = true;
            this.resultLines.push(...newSection);
        }

        this.currentSection = { name: '', hash: '', lines: [] };
    }

    async process(): Promise<void> {
        const content = await fse.readFile(this.filepath, 'utf-8');
        const lines = content.split(/\r?\n/).map(line => line + '\n');

        // Skip if already converted
        if (lines.some(line => /\$\\SRMI\\vertex_count/i.test(line))) {
            console.log(`[INFO] Skipping ${this.filepath}: already contains converted.`);
            return;
        }

        // Check if file contains vertex info
        const hasVertexInfo = lines.some(line =>
            /\$\\SRMI\\(vertex_[a-z]+|vertcount)/i.test(line)
        );
        if (!hasVertexInfo) {
            console.log(`[INFO] Invalid ini: ${this.filepath} does not contain old pipeline`);
            return;
        }

        this.modified = false;

        // First pass: find vertex_limit_max
        for (let i = 0; i < lines.length; i++) {
            const line = lines[i];
            const lineForMatch = line.replace('$\\SRMI\\vertcount', '$\\SRMI\\vertex_count');
            const vertcountMatch = lineForMatch.match(/\$\\SRMI\\(vertex_[a-z]+|vertcount)\s*=\s*(\d+)/i);
            if (vertcountMatch) {
                const vertcount = parseInt(vertcountMatch[2]);
                if (vertcount > this.vertexLimitMax) {
                    this.vertexLimitMax = vertcount;
                    console.log(`[INFO] Updated vertex_limit_max to ${this.vertexLimitMax} (line ${i + 1})`);
                }
            }
        }

        // Second pass: parse sections
        for (const line of lines) {
            const lineForMatch = line.replace('$\\SRMI\\vertcount', '$\\SRMI\\vertex_count');
            const sectionMatch = line.match(/^\[(.+?)\]/);

            if (sectionMatch) {
                if (this.currentSection.lines.length > 0) {
                    this.allSections.push({ ...this.currentSection });
                    this.currentSection = { name: '', hash: '', lines: [] };
                }
                this.currentSection.name = sectionMatch[1];
                this.currentSection.lines = [line];
                this.currentSection.hash = '';
            } else {
                this.currentSection.lines.push(line);
                if (line.toLowerCase().startsWith('hash =')) {
                    const [, h] = line.split('=', 2);
                    this.currentSection.hash = h.trim().toLowerCase();
                }

                const vertcountMatch = lineForMatch.match(/\$\\SRMI\\(vertex_[a-z]+|vertcount)\s*=\s*(\d+)/i);
                if (vertcountMatch && this.currentSection.hash) {
                    const vertcountStr = vertcountMatch[2];
                    let vertcount = 0;
                    try {
                        vertcount = parseInt(vertcountStr);
                    } catch {
                        vertcount = 0;
                    }

                    this.vertcountByHash[this.currentSection.hash] = vertcountStr;
                    if (vertcount > this.highestVertcount) {
                        this.highestVertcount = vertcount;
                    }
                    console.log(`[INFO] vertex_count = ${vertcount} for hash ${this.currentSection.hash}`);
                }
            }
        }

        if (this.currentSection.lines.length > 0) {
            this.allSections.push({ ...this.currentSection });
        }

        this.detectMergedMod();

        for (const sec of this.allSections) {
            this.currentSection = sec;
            this.flushSection();
        }

        if (!this.modified) {
            console.log(`[INFO] No changes detected in ${this.filepath}, skipping write and backup.`);
            return;
        }

        const backup = IniProcessor.backupPath(this.filepath);
        await fse.copyFile(this.filepath, backup);
        console.log(`→ Backup created: ${backup}`);

        await fse.writeFile(this.filepath, this.resultLines.join(''), 'utf-8');
    }
}

class IniBatchProcessor {
    private folder: string;

    constructor(folder: string) {
        this.folder = folder;
    }

    async processAll(): Promise<void> {
        const processDirectory = async (dir: string): Promise<void> => {
            const entries = await fse.readdir(dir, { withFileTypes: true });

            for (const entry of entries) {
                const fullPath = path.join(dir, entry.name);

                if (entry.isDirectory()) {
                    await processDirectory(fullPath);
                } else if (entry.isFile() &&
                    entry.name.toLowerCase().endsWith('.ini') &&
                    !entry.name.toLowerCase().includes('disabled')) {
                    const processor = new IniProcessor(fullPath);
                    await processor.process();
                }
            }
        };

        await processDirectory(this.folder);
    }
}

export async function HSRPipelineConvert(folderPath: string): Promise<void> {
    const processor = new IniBatchProcessor(folderPath);
    await processor.processAll();
}

// For direct execution
export async function main(): Promise<void> {
    const processor = new IniBatchProcessor(process.cwd());
    await processor.processAll();
}

// If running directly
if (require.main === module) {
    main().catch(console.error);
}