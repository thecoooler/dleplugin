<?php

define("ROOT_DIR", ".");
define("ENGINE_DIR", ROOT_DIR . "/engine");
define("TEMPLATE_DIR", "");
define("PREFIX", "");

define("DATALIFEENGINE", true);
define("USERPREFIX", "dle");
define("NEWS_ID", 1);

$config = [
    "skin" => "default",
    "date_adjust" => "UTC",
    "langs" => "English",
    "cache_type" => "0",
];
$member_id = [];
$is_loged = false;
$is_logged = false;
$user_group = [];
$cat_info = [];

$autoloadPath = __DIR__ . "/vendor/autoload.php";
if (file_exists($autoloadPath)) {
    require_once $autoloadPath;
}

$testAutoloadPath = "/test-vendor/autoload.php";
if (file_exists($testAutoloadPath)) {
    require_once $testAutoloadPath;
}


function clear_content(string $content, int $len = 300, bool $replace_single_quote = true): string
{
    return "";
}

function get_keywords_from_clear_content(string $story): string
{
    return "";
}


/**
 * @param string $file
 * @param array<array-key, array-value> $data
 * @return void
 */
function set_vars($file, $data) {}

/** @param false|string $sid */
function dle_session($sid = false)
{
    return $sid ?: "";
}

/** @return array<array-key, array{name: string}> */
function get_folder_list($folder = "language")
{
    return [];
}

/**
 * @param string $file
 * @return array<array-key, array-value>|false
 */
function get_vars($file)
{
    return false;
}

/**
 * @param string $value
 * @param bool $a
 * @param bool $b;
 * @return string
 */
function totranslit($value, $a, $b)
{
    return "";
}

/**
 * @param string $prefix
 * @param string $cache_id
 * @param bool $member_prefix
 * @param int $customCacheDate
 * @return string|false
 */
function dle_cache(
    $prefix,
    $cache_id = false,
    $member_prefix = false,
    $customCacheDate = false
) {
    return "";
}

/**
 * @param string|string[]|false $cache_areas
 * @return bool
 */
function clear_cache($cache_areas = false)
{
    return true;
}

/**
 * @param string $prefix
 * @param string $cache_text
 * @param string $cache_id
 * @param bool $member_prefix
 * @param int $maxAge
 * @return bool
 */
function create_cache(
    $prefix,
    $cache_text,
    $cache_id = false,
    $member_prefix = false,
    $maxAge = false
) {
    return "";
}

/**
 * @param string $format
 * @param int $stamp
 * @param bool $servertime
 * @param array<string, string> $custom
 * @return string
 */
function langdate($format, $stamp, $servertime = false, $custom = false)
{
    return "";
}

/**
 * @param bool $profile
 * @return array<string[]>
 */
function xfieldsload($profile = false)
{
    return [];
}

/**
 * @param string $xfields
 * @return array<string, string>|null
 */
function xfieldsdataload($xfields)
{
    return [];
}

/**
 * @param int $id
 * @return string
 */
function get_url($id)
{
    return "";
}

/**
 * @param array<array-key, string> $matches
 * @return string
 */
function custom_print($matches = [])
{
    return "";
}

/**
 * @param string $a
 * @param string|array<string, string> $b
 * @return void
 */
function echoheader($a, $b) {}

/**
 * @return void
 */
function echofooter() {}

class dle_template
{
    /**
     * @var string
     */
    public $dir = "";

    /**
     * @var string
     */
    public $template = "";

    /**
     * @var string
     */
    public $copy_template = "";

    /**
     * @var array<string, string>
     */
    public $result = [];

    /**
     * @param string $name
     * @param string|array<string, string> $var
     * @return void
     */
    function set($name, $var) {}

    /**
     * @param string $name
     * @param string|array<string, string> $var
     * @return void
     */
    function set_block($name, $var) {}

    /**
     * @param string $tpl_name
     * @return string|true
     */
    function load_template($tpl_name)
    {
        return true;
    }

    /**
     * @param string $tpl
     * @param bool $compile_if
     * @param bool $compile_user_data
     * @return void
     */
    function compile($tpl, $compile_if = false, $compile_user_data = true) {}
}

abstract class DLEPlugins
{
    /**
     * @param string $source
     * @return string
     */
    public static function Check($source)
    {
        return $source;
    }
}

class db
{
    /** @var int */
    public $query_num = 0;

    /**
     * @var array<array{"query":string,"error":string}>
     */
    public $query_errors_list = [];

    /**
     * @var array<array-key, mixed>
     */
    public $query_list = [];

    /**
     * @param string $query
     * @param boolean $show_error
     * @param boolean $log_query
     * @return \mysqli_result|bool
     */
    function query($query, $show_error = true, $log_query = true)
    {
        return true;
    }

    /**
     * @param \mysqli_result|bool|null $query_id
     * @return array<string, scalar|null>|null|false
     */
    function get_row($query_id = null)
    {
        return [];
    }

    /**
     * @param \mysqli_result|bool|null $query_id
     * @return void
     */
    function free($query_id = null) {}

    /**
     * @return string|int
     */
    function get_affected_rows()
    {
        return 0;
    }

    /**
     * @param string $query
     * @param boolean $multi
     * @param boolean $show_error
     * @param boolean $log_query
     * @return array<array-key, string|int|float|null|bool>|array<array<array-key, string|int|float|null|bool>>
     */
    function super_query(
        $query,
        $multi = false,
        $show_error = true,
        $log_query = true
    ) {
        return [];
    }

    /**
     * @return int|string
     */
    function insert_id()
    {
        return 0;
    }

    /**
     * @param string $source
     * @return string
     */
    function safesql($source)
    {
        return $source;
    }

    /**
     * @return void
     */
    function close() {}
}

class ParseFilter
{
    public bool $not_allowed_text = false;
    public bool $allow_code = true;

    public function process(string $value): string
    {
        return "";
    }

    public function BB_Parse(string $source, bool $use_html = true): string
    {
        return "";
    }

    public function decodeBBCodes(string $txt, bool $use_html = true, bool $wysiwig = false): string
    {
        return "";
    }
}

class microTimer
{
    public function get(): float
    {
        return 0.0;
    }
}

class DLECountry
{
    public static function Get(?string $ip = null): string
    {
        return $ip ?? "";
    }
}

$tpl = new dle_template();
$db = new db();
$Timer = new microTimer();
