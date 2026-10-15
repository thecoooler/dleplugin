<?php

declare(strict_types=1);

namespace Acme\integrationsmoke;

final class Probe
{
    public static function answer(): int
    {
        return 42;
    }
}
