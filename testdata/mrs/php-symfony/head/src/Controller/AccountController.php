<?php

namespace App\Controller;

use Symfony\Component\HttpFoundation\Response;
use Symfony\Component\Routing\Attribute\Route;

#[Route('/account')]
class AccountController
{
    /**
     * Shows an account.
     */
    #[Route('/{id}', name: 'account_show')]
    public function show(int $id): Response
    {
        $sql = "SELECT * FROM account WHERE id = " . $id;

        return new Response($sql);
    }

    public function list(): Response
    {
        return new Response('list');
    }
}
