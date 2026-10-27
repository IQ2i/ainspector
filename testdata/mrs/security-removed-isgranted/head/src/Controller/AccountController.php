<?php

namespace App\Controller;

use App\Entity\Account;
use Symfony\Component\HttpFoundation\Response;
use Symfony\Component\Routing\Attribute\Route;
use Symfony\Component\Security\Http\Attribute\IsGranted;

#[Route('/accounts')]
class AccountController extends AbstractController
{
    #[Route('/{id}', name: 'account_show')]
    #[IsGranted('ACCOUNT_VIEW', 'account')]
    public function show(Account $account): Response
    {
        return $this->render('account/show.html.twig', ['account' => $account]);
    }

    #[Route('/{id}/edit', name: 'account_edit')]
    public function edit(Account $account): Response
    {
        return $this->render('account/edit.html.twig', ['account' => $account]);
    }
}
